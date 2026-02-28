package iscsi

import (
	"encoding/binary"
	"net"
)

// handleNopOut processes a NOP-Out PDU and sends a NOP-In response.
// Per RFC 7143: if ITT != 0xFFFFFFFF, target must respond with NOP-In.
func handleNopOut(conn net.Conn, req *PDU, statSN, expCmdSN, maxCmdSN uint32) error {
	itt := req.InitiatorTaskTag()

	// If ITT == 0xFFFFFFFF, this is an unsolicited NOP-Out (ping). Still respond.
	// If ITT != 0xFFFFFFFF, this is a solicited NOP-Out requiring a response.

	rsp := &PDU{}
	rsp.BHS[0] = OpcodeNopIn
	rsp.BHS[1] = 0x80 // F bit always set for NOP-In

	// Copy LUN from request
	copy(rsp.BHS[8:16], req.BHS[8:16])

	// Echo ITT back
	rsp.SetInitiatorTaskTag(itt)

	// TTT = 0xFFFFFFFF (not solicited)
	binary.BigEndian.PutUint32(rsp.BHS[20:24], 0xFFFFFFFF)

	// StatSN
	rsp.SetStatSN(statSN)
	rsp.SetExpCmdSN(expCmdSN)
	rsp.SetMaxCmdSN(maxCmdSN)

	// Echo any data segment
	if len(req.DataSegment) > 0 {
		rsp.DataSegment = make([]byte, len(req.DataSegment))
		copy(rsp.DataSegment, req.DataSegment)
	}

	return WritePDU(conn, rsp)
}
