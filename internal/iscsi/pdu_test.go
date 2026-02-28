package iscsi

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPDUOpcodeAccessors(t *testing.T) {
	pdu := &PDU{}

	pdu.SetOpcode(OpcodeSCSICmd)
	assert.Equal(t, OpcodeSCSICmd, pdu.Opcode())

	pdu.SetOpcode(OpcodeLoginReq)
	assert.Equal(t, OpcodeLoginReq, pdu.Opcode())
}

func TestPDUFlagsAccessors(t *testing.T) {
	pdu := &PDU{}
	pdu.SetFlags(0xF0)
	assert.Equal(t, byte(0xF0), pdu.Flags())
}

func TestPDUDataSegmentLength(t *testing.T) {
	pdu := &PDU{}
	pdu.SetDataSegmentLength(0x123456)
	assert.Equal(t, uint32(0x123456), pdu.DataSegmentLength())
	// Verify bytes 5-7
	assert.Equal(t, byte(0x12), pdu.BHS[5])
	assert.Equal(t, byte(0x34), pdu.BHS[6])
	assert.Equal(t, byte(0x56), pdu.BHS[7])
}

func TestPDUInitiatorTaskTag(t *testing.T) {
	pdu := &PDU{}
	pdu.SetInitiatorTaskTag(0xDEADBEEF)
	assert.Equal(t, uint32(0xDEADBEEF), pdu.InitiatorTaskTag())
}

func TestPDULUN(t *testing.T) {
	pdu := &PDU{}
	pdu.SetLUN(0x0001000000000000)
	assert.Equal(t, uint64(0x0001000000000000), pdu.LUN())
}

func TestPDUCmdSN(t *testing.T) {
	pdu := &PDU{}
	pdu.SetCmdSN(12345)
	assert.Equal(t, uint32(12345), pdu.CmdSN())
}

func TestPDUStatSN(t *testing.T) {
	pdu := &PDU{}
	pdu.SetStatSN(99)
	assert.Equal(t, uint32(99), pdu.StatSN())
}

func TestPDUExpCmdSN(t *testing.T) {
	pdu := &PDU{}
	pdu.SetExpCmdSN(500)
	assert.Equal(t, uint32(500), pdu.ExpCmdSN())
}

func TestPDUMaxCmdSN(t *testing.T) {
	pdu := &PDU{}
	pdu.SetMaxCmdSN(1000)
	assert.Equal(t, uint32(1000), pdu.MaxCmdSN())
}

func TestReadWritePDURoundtrip(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Build a test PDU
	orig := &PDU{}
	orig.SetOpcode(OpcodeNopOut)
	orig.SetFlags(0x80)
	orig.SetInitiatorTaskTag(0x12345678)
	orig.SetCmdSN(1)
	orig.DataSegment = []byte("hello iSCSI!")

	// Write in goroutine
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- WritePDU(client, orig)
	}()

	// Read
	got, err := ReadPDU(server)
	require.NoError(t, err)
	require.NoError(t, <-writeErr)

	assert.Equal(t, OpcodeNopOut, got.Opcode())
	assert.Equal(t, byte(0x80), got.Flags())
	assert.Equal(t, uint32(0x12345678), got.InitiatorTaskTag())
	assert.Equal(t, orig.DataSegment, got.DataSegment)
}

func TestReadWritePDUNoPadding(t *testing.T) {
	// DataSegment already multiple of 4
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	orig := &PDU{}
	orig.SetOpcode(OpcodeDataOut)
	orig.DataSegment = []byte{0x01, 0x02, 0x03, 0x04} // 4 bytes, no padding needed

	go func() { _ = WritePDU(client, orig) }()

	got, err := ReadPDU(server)
	require.NoError(t, err)
	assert.Equal(t, orig.DataSegment, got.DataSegment)
}

func TestReadWritePDUPadding(t *testing.T) {
	// DataSegment needs 1 byte of padding (3 + 1 = 4)
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	orig := &PDU{}
	orig.SetOpcode(OpcodeTextReq)
	orig.DataSegment = []byte{0xAA, 0xBB, 0xCC} // 3 bytes -> padded to 4

	go func() { _ = WritePDU(client, orig) }()

	got, err := ReadPDU(server)
	require.NoError(t, err)
	assert.Equal(t, []byte{0xAA, 0xBB, 0xCC}, got.DataSegment)
}

func TestReadWritePDUNoData(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	orig := &PDU{}
	orig.SetOpcode(OpcodeLogoutReq)
	orig.SetFlags(0x80)

	go func() { _ = WritePDU(client, orig) }()

	got, err := ReadPDU(server)
	require.NoError(t, err)
	assert.Equal(t, OpcodeLogoutReq, got.Opcode())
	assert.Nil(t, got.DataSegment)
}

func TestReadPDUBinaryLayout(t *testing.T) {
	// Build raw bytes manually and verify ReadPDU parses correctly
	raw := make([]byte, 48)
	raw[0] = OpcodeLoginReq    // opcode
	raw[1] = 0xC7              // flags
	raw[4] = 0x00              // no AHS
	// DataSegmentLength = 8 (3 bytes at bytes 5-7)
	raw[5] = 0x00
	raw[6] = 0x00
	raw[7] = 0x08
	// ITT at bytes 16-19
	binary.BigEndian.PutUint32(raw[16:20], 0xAABBCCDD)

	// DataSegment (8 bytes)
	ds := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		_, _ = client.Write(raw)
		_, _ = client.Write(ds) // already 4-byte aligned
	}()

	pdu, err := ReadPDU(server)
	require.NoError(t, err)
	assert.Equal(t, OpcodeLoginReq, pdu.Opcode())
	assert.Equal(t, byte(0xC7), pdu.Flags())
	assert.Equal(t, uint32(0xAABBCCDD), pdu.InitiatorTaskTag())
	assert.Equal(t, ds, pdu.DataSegment)
}

func TestOpcodeConstants(t *testing.T) {
	// Verify opcode values against RFC 7143
	assert.Equal(t, byte(0x00), OpcodeNopOut)
	assert.Equal(t, byte(0x01), OpcodeSCSICmd)
	assert.Equal(t, byte(0x02), OpcodeTMFReq)
	assert.Equal(t, byte(0x03), OpcodeLoginReq)
	assert.Equal(t, byte(0x04), OpcodeTextReq)
	assert.Equal(t, byte(0x05), OpcodeDataOut)
	assert.Equal(t, byte(0x06), OpcodeLogoutReq)
	assert.Equal(t, byte(0x20), OpcodeNopIn)
	assert.Equal(t, byte(0x21), OpcodeSCSIRsp)
	assert.Equal(t, byte(0x22), OpcodeTMFRsp)
	assert.Equal(t, byte(0x23), OpcodeLoginRsp)
	assert.Equal(t, byte(0x24), OpcodeTextRsp)
	assert.Equal(t, byte(0x25), OpcodeDataIn)
	assert.Equal(t, byte(0x26), OpcodeLogoutRsp)
	assert.Equal(t, byte(0x31), OpcodeR2T)
	assert.Equal(t, byte(0x3F), OpcodeReject)
}

// TestReadPDUError verifies an error is returned when the connection is closed.
func TestReadPDUError(t *testing.T) {
	client, server := net.Pipe()
	client.Close() // close immediately

	_, err := ReadPDU(server)
	assert.Error(t, err)
	server.Close()
}

// TestWritePDUBinaryContent verifies the exact bytes written to a buffer.
func TestWritePDUBinaryContent(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	pdu := &PDU{}
	pdu.SetOpcode(OpcodeNopIn)
	pdu.SetInitiatorTaskTag(0x00000001)
	// No data segment

	var buf []byte
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		tmp := make([]byte, 48)
		_, _ = server.Read(tmp)
		buf = tmp
	}()

	err := WritePDU(client, pdu)
	require.NoError(t, err)
	client.Close()
	<-readDone

	require.Len(t, buf, 48)
	assert.Equal(t, OpcodeNopIn, buf[0]&0x3F)
	// DataSegmentLength should be 0
	assert.Equal(t, byte(0), buf[5])
	assert.Equal(t, byte(0), buf[6])
	assert.Equal(t, byte(0), buf[7])
}

func makePDU(opcode byte, ds []byte) *PDU {
	p := &PDU{}
	p.SetOpcode(opcode)
	p.DataSegment = ds
	return p
}

func mustReadAll(r net.Conn, n int) []byte {
	buf := make([]byte, n)
	total := 0
	for total < n {
		nn, err := r.Read(buf[total:])
		if err != nil {
			return buf[:total]
		}
		total += nn
	}
	return buf
}

func TestWritePDUPaddingBytes(t *testing.T) {
	// DataSegment of 5 bytes requires 3 padding bytes (total 8)
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	pdu := makePDU(OpcodeNopOut, []byte{1, 2, 3, 4, 5})

	var received []byte
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 48 (BHS) + 8 (5 data + 3 pad)
		received = mustReadAll(server, 56)
	}()

	err := WritePDU(client, pdu)
	require.NoError(t, err)
	<-done

	require.Len(t, received, 56)
	// DataSegmentLength field (bytes 5-7) = 5
	dsLen := uint32(received[5])<<16 | uint32(received[6])<<8 | uint32(received[7])
	assert.Equal(t, uint32(5), dsLen)
	// Data bytes
	assert.Equal(t, []byte{1, 2, 3, 4, 5}, received[48:53])
	// Padding bytes should be 0
	assert.Equal(t, []byte{0, 0, 0}, received[53:56])
}

// Verify BHSSize constant
func TestBHSSize(t *testing.T) {
	assert.Equal(t, 48, BHSSize)
	var p PDU
	assert.Equal(t, BHSSize, len(p.BHS))
}

func TestExpStatSN(t *testing.T) {
	p := &PDU{}
	p.SetExpStatSN(42)
	assert.Equal(t, uint32(42), p.ExpStatSN())
}

// Helper to build bytes for manual testing
func buildRawBHS(opcode, flags byte, ahsLen byte, dsLen uint32, itt uint32) []byte {
	raw := make([]byte, 48)
	raw[0] = opcode
	raw[1] = flags
	raw[4] = ahsLen
	raw[5] = byte(dsLen >> 16)
	raw[6] = byte(dsLen >> 8)
	raw[7] = byte(dsLen)
	binary.BigEndian.PutUint32(raw[16:20], itt)
	return raw
}

func TestReadPDUWithAHS(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// Build raw PDU with 4-byte AHS and 4-byte data segment
	raw := buildRawBHS(OpcodeSCSICmd, 0x00, 1 /*AHS units = 4 bytes*/, 4, 0xBEEF)

	go func() {
		_, _ = client.Write(raw)
		_, _ = client.Write([]byte{0x01, 0x02, 0x03, 0x04}) // AHS (4 bytes)
		_, _ = client.Write([]byte{0xAA, 0xBB, 0xCC, 0xDD}) // DataSegment (4 bytes, aligned)
	}()

	pdu, err := ReadPDU(server)
	require.NoError(t, err)
	assert.Equal(t, OpcodeSCSICmd, pdu.Opcode())
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, pdu.AHS)
	assert.Equal(t, []byte{0xAA, 0xBB, 0xCC, 0xDD}, pdu.DataSegment)
}

// Test that WritePDU without conn errors works
func TestWritePDUClosedConn(t *testing.T) {
	client, server := net.Pipe()
	server.Close()

	pdu := makePDU(OpcodeNopOut, nil)
	err := WritePDU(client, pdu)
	assert.Error(t, err)
	client.Close()
}

// Use bytes.Buffer to verify written content without network
func TestPDUWriteToBuffer(t *testing.T) {
	// net.Pipe is bidirectional; use it to capture output
	client, server := net.Pipe()
	defer server.Close()

	pdu := &PDU{}
	pdu.SetOpcode(OpcodeSCSIRsp)
	pdu.SetFlags(0x80)
	pdu.SetInitiatorTaskTag(0xFFFFFFFF)
	pdu.DataSegment = []byte("test")

	go func() { _ = WritePDU(client, pdu) }()

	result, err := ReadPDU(server)
	require.NoError(t, err)
	assert.Equal(t, OpcodeSCSIRsp, result.Opcode())
	assert.Equal(t, byte(0x80), result.Flags())
	assert.Equal(t, uint32(0xFFFFFFFF), result.InitiatorTaskTag())
	assert.Equal(t, []byte("test"), result.DataSegment)
}

// Suppress unused import
var _ = bytes.NewBuffer
