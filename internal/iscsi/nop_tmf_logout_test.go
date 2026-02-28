package iscsi

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NOP tests ---

func TestHandleNopOut(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Build NOP-Out
	req := &PDU{}
	req.SetOpcode(OpcodeNopOut)
	req.SetFlags(0x80)
	req.SetInitiatorTaskTag(0x12345678)
	req.DataSegment = []byte("ping")

	done := make(chan error, 1)
	go func() {
		done <- handleNopOut(server, req, 1, 2, 100)
	}()

	// Read NOP-In
	rsp, err := ReadPDU(client)
	require.NoError(t, err)
	require.NoError(t, <-done)

	assert.Equal(t, OpcodeNopIn, rsp.Opcode())
	assert.Equal(t, byte(0x80), rsp.Flags())
	assert.Equal(t, uint32(0x12345678), rsp.InitiatorTaskTag())
	assert.Equal(t, []byte("ping"), rsp.DataSegment)
}

func TestHandleNopOutStatSN(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	req := &PDU{}
	req.SetOpcode(OpcodeNopOut)
	req.SetFlags(0x80)
	req.SetInitiatorTaskTag(1)

	go func() { _ = handleNopOut(server, req, 42, 10, 50) }()

	rsp, err := ReadPDU(client)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), rsp.StatSN())
	assert.Equal(t, uint32(10), rsp.ExpCmdSN())
	assert.Equal(t, uint32(50), rsp.MaxCmdSN())
}

func TestHandleNopOutNoData(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	req := &PDU{}
	req.SetOpcode(OpcodeNopOut)
	req.SetFlags(0x80)
	req.SetInitiatorTaskTag(0xFFFFFFFF)
	// No data segment

	go func() { _ = handleNopOut(server, req, 1, 0, 31) }()

	rsp, err := ReadPDU(client)
	require.NoError(t, err)
	assert.Equal(t, OpcodeNopIn, rsp.Opcode())
	assert.Nil(t, rsp.DataSegment)
}

// --- TMF tests ---

func TestHandleTMFAbortTask(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	req := &PDU{}
	req.SetOpcode(OpcodeTMFReq)
	req.BHS[1] = TMFFuncAbortTask | 0x80 // F bit + function
	req.SetInitiatorTaskTag(0xAABBCCDD)

	go func() { _ = handleTMFRequest(server, req, 1, 0, 31) }()

	rsp, err := ReadPDU(client)
	require.NoError(t, err)
	assert.Equal(t, OpcodeTMFRsp, rsp.Opcode())
	assert.Equal(t, TMFResponseFunctionComplete, rsp.BHS[2])
	assert.Equal(t, uint32(0xAABBCCDD), rsp.InitiatorTaskTag())
}

func TestHandleTMFLUNReset(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	req := &PDU{}
	req.SetOpcode(OpcodeTMFReq)
	req.BHS[1] = TMFFuncLogicalUnitReset | 0x80
	req.SetInitiatorTaskTag(1)

	go func() { _ = handleTMFRequest(server, req, 1, 0, 31) }()

	rsp, err := ReadPDU(client)
	require.NoError(t, err)
	assert.Equal(t, TMFResponseFunctionComplete, rsp.BHS[2])
}

func TestHandleTMFUnsupported(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	req := &PDU{}
	req.SetOpcode(OpcodeTMFReq)
	req.BHS[1] = 0x7F // Unknown function code
	req.SetInitiatorTaskTag(1)

	go func() { _ = handleTMFRequest(server, req, 1, 0, 31) }()

	rsp, err := ReadPDU(client)
	require.NoError(t, err)
	assert.Equal(t, TMFResponseUnsupported, rsp.BHS[2])
}

// --- Logout tests ---

func TestHandleLogout(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	req := &PDU{}
	req.SetOpcode(OpcodeLogoutReq)
	req.BHS[1] = 0x80 | LogoutReasonCloseSession
	req.SetInitiatorTaskTag(0xDEAD)

	go func() { _ = handleLogout(server, req, 5, 2, 33) }()

	rsp, err := ReadPDU(client)
	require.NoError(t, err)
	assert.Equal(t, OpcodeLogoutRsp, rsp.Opcode())
	assert.Equal(t, byte(0x80), rsp.BHS[1])
	assert.Equal(t, LogoutResponseSuccess, rsp.BHS[2])
	assert.Equal(t, uint32(0xDEAD), rsp.InitiatorTaskTag())
	assert.Equal(t, uint32(5), rsp.StatSN())
}

func TestHandleLogoutClosesAfterResponse(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	req := &PDU{}
	req.SetOpcode(OpcodeLogoutReq)
	req.BHS[1] = 0x80 | LogoutReasonCloseSession
	req.SetInitiatorTaskTag(1)

	done := make(chan error, 1)
	go func() {
		done <- handleLogout(server, req, 1, 0, 31)
	}()

	rsp, err := ReadPDU(client)
	require.NoError(t, err)
	assert.Equal(t, OpcodeLogoutRsp, rsp.Opcode())

	err = <-done
	assert.NoError(t, err)
}
