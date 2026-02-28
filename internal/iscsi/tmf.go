package iscsi

import (
	"encoding/binary"
	"net"
)

// TMF function codes (RFC 7143 Table 11).
const (
	TMFFuncAbortTask         byte = 0x01
	TMFFuncAbortTaskSet      byte = 0x02
	TMFFuncClearACA          byte = 0x03
	TMFFuncClearTaskSet      byte = 0x04
	TMFFuncLogicalUnitReset  byte = 0x05
	TMFFuncTargetWarmReset   byte = 0x06
	TMFFuncTargetColdReset   byte = 0x07
	TMFFuncTaskReassign      byte = 0x08
	TMFFuncQueryTask         byte = 0x09
	TMFFuncQueryTaskSet      byte = 0x0A
	TMFFuncQueryAsyncEvent   byte = 0x0C
)

// TMF response codes.
const (
	TMFResponseFunctionComplete     byte = 0x00
	TMFResponseTaskDoesNotExist     byte = 0x01
	TMFResponseLUNDoesNotExist      byte = 0x02
	TMFResponseTaskStillAllegiant   byte = 0x03
	TMFResponseTaskAllegiance       byte = 0x04
	TMFResponseUnsupported          byte = 0x05
	TMFResponseAuthorizationFailed  byte = 0x06
	TMFResponseFunctionRejected     byte = 0xFF
)

// handleTMFRequest processes a Task Management Function Request PDU.
// This stub implementation returns FUNCTION COMPLETE for all supported functions.
func handleTMFRequest(conn net.Conn, req *PDU, statSN, expCmdSN, maxCmdSN uint32) error {
	// TMF function is in bits 6-0 of byte 1
	function := req.BHS[1] & 0x7F

	var response byte
	switch function {
	case TMFFuncAbortTask, TMFFuncAbortTaskSet, TMFFuncClearTaskSet,
		TMFFuncLogicalUnitReset, TMFFuncTargetWarmReset, TMFFuncTargetColdReset,
		TMFFuncClearACA:
		response = TMFResponseFunctionComplete
	default:
		response = TMFResponseUnsupported
	}

	rsp := &PDU{}
	rsp.BHS[0] = OpcodeTMFRsp
	rsp.BHS[1] = 0x80 // F bit
	rsp.BHS[2] = response

	// Copy ITT from request
	rsp.SetInitiatorTaskTag(req.InitiatorTaskTag())

	// RTTTT (Referenced Task Tag) = 0xFFFFFFFF
	binary.BigEndian.PutUint32(rsp.BHS[20:24], 0xFFFFFFFF)

	rsp.SetStatSN(statSN)
	rsp.SetExpCmdSN(expCmdSN)
	rsp.SetMaxCmdSN(maxCmdSN)

	return WritePDU(conn, rsp)
}
