package iscsi

import (
	"fmt"
	"net"
	"strconv"
)

// Login stage constants (RFC 7143 Section 10.12.1).
const (
	StageSecurityNegotiation  = 0
	StageOperationalNegotiation = 1
	StageFullFeaturePhase     = 3
)

// Login response status classes.
const (
	LoginStatusSuccess          byte = 0x00
	LoginStatusRedirect         byte = 0x01
	LoginStatusInitiatorError   byte = 0x02
	LoginStatusTargetError      byte = 0x03
)

// Login response status detail codes.
const (
	LoginDetailSuccess            byte = 0x00
	LoginDetailAuthFailure        byte = 0x01
	LoginDetailForbidden          byte = 0x02
	LoginDetailNotFound           byte = 0x04
	LoginDetailTargetRemoved      byte = 0x05
	LoginDetailUnsupportedVersion byte = 0x05
	LoginDetailTooManyConnections byte = 0x08
	LoginDetailMissingParam       byte = 0x09
	LoginDetailCantIncludeInSession byte = 0x0A
	LoginDetailSessionTypeNotSupported byte = 0x0B
	LoginDetailSessionDoesNotExist byte = 0x0C
	LoginDetailInvalidDuringLogin  byte = 0x0F
)

// AuthConfig holds authentication settings for the login handler.
type AuthConfig struct {
	Enabled    bool
	CHAPUser   string
	CHAPSecret string
}

// loginHandler manages the login state machine for a single connection.
type loginHandler struct {
	conn       net.Conn
	authConfig AuthConfig
	targetName string
	targetPort string

	// State
	currentStage  int
	targetStage   int
	negotiated    Params
	chapChallenge *CHAPChallenge

	// Tracking
	isid    [6]byte
	tsih    uint16
	cmdSN   uint32
	statSN  uint32
}

// newLoginHandler creates a new login handler for an incoming connection.
func newLoginHandler(conn net.Conn, auth AuthConfig, targetName string, statSN uint32) *loginHandler {
	return &loginHandler{
		conn:         conn,
		authConfig:   auth,
		targetName:   targetName,
		currentStage: StageSecurityNegotiation,
		negotiated:   DefaultTargetParams(),
		statSN:       statSN,
	}
}

// Run executes the login state machine until FullFeaturePhase is reached
// or an error occurs. Returns the negotiated session parameters.
func (h *loginHandler) Run() (Params, uint32, error) {
	for h.currentStage != StageFullFeaturePhase {
		pdu, err := ReadPDU(h.conn)
		if err != nil {
			return Params{}, 0, fmt.Errorf("reading login PDU: %w", err)
		}

		if pdu.Opcode() != OpcodeLoginReq {
			return Params{}, 0, fmt.Errorf("expected Login Request, got opcode 0x%02X", pdu.Opcode())
		}

		if err := h.handleLoginPDU(pdu); err != nil {
			return Params{}, 0, err
		}
	}
	return h.negotiated, h.cmdSN, nil
}

// handleLoginPDU processes a single Login Request PDU.
func (h *loginHandler) handleLoginPDU(req *PDU) error {
	// Parse login request BHS fields
	// Byte 1: T=bit7, C=bit6, CSG=bits2-3, NSG=bits0-1
	flags := req.BHS[1]
	transitBit := (flags & 0x80) != 0
	csg := int((flags >> 2) & 0x03) // Current Stage
	nsg := int(flags & 0x03)        // Next Stage (requested)

	// Parse key-value pairs from data segment
	var kvIn map[string]string
	if len(req.DataSegment) > 0 {
		kvIn = ParseKeyValuePairs(req.DataSegment)
	} else {
		kvIn = make(map[string]string)
	}

	// Track CmdSN
	h.cmdSN = req.CmdSN()

	// Save ISID from bytes 8-13 of BHS
	copy(h.isid[:], req.BHS[8:14])

	switch csg {
	case StageSecurityNegotiation:
		return h.handleSecurityStage(req, kvIn, transitBit, nsg)
	case StageOperationalNegotiation:
		return h.handleOperationalStage(req, kvIn, transitBit, nsg)
	default:
		return h.sendLoginError(req, LoginStatusInitiatorError, LoginDetailInvalidDuringLogin)
	}
}

// handleSecurityStage handles security negotiation.
func (h *loginHandler) handleSecurityStage(req *PDU, kvIn map[string]string, transitBit bool, nsg int) error {
	if !h.authConfig.Enabled {
		// No auth: immediately accept and transition to operational or full feature
		kvOut := map[string]string{
			"TargetName": h.targetName,
		}

		if initName, ok := kvIn["InitiatorName"]; ok {
			h.negotiated.InitiatorName = initName
		}
		if authMethod, ok := kvIn["AuthMethod"]; ok {
			_ = authMethod
			kvOut["AuthMethod"] = "None"
		}

		nextStage := StageOperationalNegotiation
		if transitBit && nsg == StageFullFeaturePhase {
			nextStage = StageFullFeaturePhase
		}

		return h.sendLoginResponse(req, LoginStatusSuccess, LoginDetailSuccess,
			StageSecurityNegotiation, nextStage, transitBit, kvOut)
	}

	// CHAP authentication
	if h.chapChallenge == nil {
		// First exchange: generate and send challenge immediately
		chap, err := NewCHAPChallenge()
		if err != nil {
			return fmt.Errorf("generating CHAP challenge: %w", err)
		}
		h.chapChallenge = chap

		if initName, ok := kvIn["InitiatorName"]; ok {
			h.negotiated.InitiatorName = initName
		}

		// Send AuthMethod=CHAP along with challenge (CHAP_I and CHAP_C)
		kvOut := map[string]string{
			"AuthMethod": "CHAP",
			"CHAP_A":     "5", // MD5
			"CHAP_I":     strconv.Itoa(int(h.chapChallenge.ID)),
			"CHAP_C":     h.chapChallenge.ChallengeHex(),
		}

		// Stay in security stage; initiator will send back CHAP_N/CHAP_R
		return h.sendLoginResponse(req, LoginStatusSuccess, LoginDetailSuccess,
			StageSecurityNegotiation, StageSecurityNegotiation, false, kvOut)
	}

	// Second exchange: verify response
	chapParams := ParseCHAPParams(kvIn)
	if !h.chapChallenge.VerifyResponse(h.authConfig.CHAPSecret, chapParams.Response) {
		return h.sendLoginError(req, LoginStatusInitiatorError, LoginDetailAuthFailure)
	}

	// Authentication successful; transition to operational stage
	kvOut := map[string]string{
		"TargetName": h.targetName,
	}
	return h.sendLoginResponse(req, LoginStatusSuccess, LoginDetailSuccess,
		StageSecurityNegotiation, StageOperationalNegotiation, true, kvOut)
}

// handleOperationalStage handles operational parameter negotiation.
func (h *loginHandler) handleOperationalStage(req *PDU, kvIn map[string]string, transitBit bool, nsg int) error {
	// Negotiate parameters
	rspKV, negotiated := NegotiateParams(kvIn, h.negotiated)
	h.negotiated = negotiated

	// Respond with target's name and negotiated parameters
	rspKV["TargetName"] = h.targetName

	nextStage := StageOperationalNegotiation
	if transitBit {
		nextStage = nsg
		if nextStage == StageFullFeaturePhase {
			h.currentStage = StageFullFeaturePhase
		}
	}

	return h.sendLoginResponse(req, LoginStatusSuccess, LoginDetailSuccess,
		StageOperationalNegotiation, nextStage, transitBit, rspKV)
}

// sendLoginResponse sends a Login Response PDU.
func (h *loginHandler) sendLoginResponse(
	req *PDU,
	statusClass, statusDetail byte,
	csg, nsg int,
	transitBit bool,
	kvOut map[string]string,
) error {
	rsp := &PDU{}
	rsp.BHS[0] = OpcodeLoginRsp

	// Flags: T bit, C=0, CSG, NSG
	flags := byte(csg<<2) | byte(nsg)
	if transitBit {
		flags |= 0x80 // T bit
		// If we're transitioning to FFP, update current stage
		if nsg == StageFullFeaturePhase {
			h.currentStage = StageFullFeaturePhase
		} else if csg == StageSecurityNegotiation && nsg == StageOperationalNegotiation {
			h.currentStage = StageOperationalNegotiation
		}
	}
	rsp.BHS[1] = flags

	// Version Max/Active (bytes 2-3): version 0x00
	rsp.BHS[2] = 0x00
	rsp.BHS[3] = 0x00

	// Copy ISID from request (bytes 8-13) and CID (bytes 14-15)
	copy(rsp.BHS[8:14], req.BHS[8:14])
	copy(rsp.BHS[14:16], req.BHS[14:16])

	// TSIH (bytes 6-7 in the target login response)
	// Actually: TSIH is at bytes 14-15 of the login response BHS
	// Per RFC 7143 Table 12: BHS for Login Response:
	//   bytes 8-13: ISID
	//   bytes 14-15: TSIH (assigned by target)
	// Note: For simplicity we use TSIH=1 for all sessions
	rsp.BHS[14] = 0x00
	rsp.BHS[15] = 0x01

	// InitiatorTaskTag from request
	copy(rsp.BHS[16:20], req.BHS[16:20])

	// CmdSN/StatSN/ExpCmdSN
	h.statSN++
	rsp.SetStatSN(h.statSN)
	rsp.SetExpCmdSN(h.cmdSN + 1)
	rsp.SetMaxCmdSN(h.cmdSN + 32)

	// Status class and detail (bytes 36-37)
	rsp.BHS[36] = statusClass
	rsp.BHS[37] = statusDetail

	// Serialize response parameters
	if len(kvOut) > 0 {
		rsp.DataSegment = SerializeKeyValuePairs(kvOut)
	}

	return WritePDU(h.conn, rsp)
}

// sendLoginError sends a Login Response with an error status.
func (h *loginHandler) sendLoginError(req *PDU, statusClass, statusDetail byte) error {
	return h.sendLoginResponse(req, statusClass, statusDetail,
		h.currentStage, h.currentStage, false, nil)
}
