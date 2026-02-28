package iscsi

import "net"

// Logout reason codes (RFC 7143 Section 10.14).
const (
	LogoutReasonCloseSession    byte = 0x00
	LogoutReasonCloseConnection byte = 0x01
	LogoutReasonRemoveConnection byte = 0x02
)

// Logout response codes (RFC 7143 Section 10.15).
const (
	LogoutResponseSuccess     byte = 0x00
	LogoutResponseCIDNotFound byte = 0x01
	LogoutResponseRecoveryNotSupported byte = 0x02
	LogoutResponseCleanupFailed byte = 0x03
)

// handleLogout processes a Logout Request PDU and sends a Logout Response.
// After sending the response, the caller should close the connection.
func handleLogout(conn net.Conn, req *PDU, statSN, expCmdSN, maxCmdSN uint32) error {
	rsp := &PDU{}
	rsp.BHS[0] = OpcodeLogoutRsp
	rsp.BHS[1] = 0x80 // F bit always set
	rsp.BHS[2] = LogoutResponseSuccess

	// Copy ITT from request
	rsp.SetInitiatorTaskTag(req.InitiatorTaskTag())

	rsp.SetStatSN(statSN)
	rsp.SetExpCmdSN(expCmdSN)
	rsp.SetMaxCmdSN(maxCmdSN)

	// Time2Wait and Time2Retain (bytes 40-43, 44-47): 0
	// bytes 40-41: Time2Wait
	rsp.BHS[40] = 0x00
	rsp.BHS[41] = 0x00
	// bytes 42-43: Time2Retain
	rsp.BHS[42] = 0x00
	rsp.BHS[43] = 0x00

	return WritePDU(conn, rsp)
}
