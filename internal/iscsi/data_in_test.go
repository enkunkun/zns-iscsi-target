package iscsi

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendDataInSinglePDU(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	data := make([]byte, 512)
	for i := range data {
		data[i] = byte(i % 256)
	}

	done := make(chan error, 1)
	go func() {
		done <- sendDataIn(server, 0x1234, 0, data, 1, 0, 31, 65536)
	}()

	// Receive one Data-In PDU
	pdu, err := ReadPDU(client)
	require.NoError(t, err)
	require.NoError(t, <-done)

	assert.Equal(t, OpcodeDataIn, pdu.Opcode())
	// F bit should be set (final)
	assert.Equal(t, byte(0x80), pdu.Flags()&0x80)
	assert.Equal(t, uint32(0x1234), pdu.InitiatorTaskTag())
	assert.Equal(t, data, pdu.DataSegment)

	// DataSN should be 0
	dataSN := binary.BigEndian.Uint32(pdu.BHS[36:40])
	assert.Equal(t, uint32(0), dataSN)
}

func TestSendDataInMultiplePDUs(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	// 3 * maxRecv = 3 PDUs
	maxRecv := 4
	data := make([]byte, 12)
	for i := range data {
		data[i] = byte(i)
	}

	done := make(chan error, 1)
	go func() {
		done <- sendDataIn(server, 0xABCD, 0, data, 1, 0, 31, maxRecv)
	}()

	// Read 3 PDUs
	var pdus []*PDU
	for i := 0; i < 3; i++ {
		pdu, err := ReadPDU(client)
		require.NoError(t, err)
		pdus = append(pdus, pdu)
	}

	require.NoError(t, <-done)
	require.Len(t, pdus, 3)

	// Verify DataSN increments
	for i, pdu := range pdus {
		dataSN := binary.BigEndian.Uint32(pdu.BHS[36:40])
		assert.Equal(t, uint32(i), dataSN)
	}

	// Only last PDU has F bit set
	for i, pdu := range pdus {
		finalBit := (pdu.Flags() & 0x80) != 0
		if i == len(pdus)-1 {
			assert.True(t, finalBit, "last PDU must have F bit set")
		} else {
			assert.False(t, finalBit, "intermediate PDU must NOT have F bit set")
		}
	}

	// Verify data content
	var assembled []byte
	for _, pdu := range pdus {
		assembled = append(assembled, pdu.DataSegment...)
	}
	assert.Equal(t, data, assembled)
}

func TestSendDataInBufferOffset(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	data := make([]byte, 8)
	for i := range data {
		data[i] = byte(i)
	}

	done := make(chan error, 1)
	go func() {
		done <- sendDataIn(server, 1, 0, data, 1, 0, 31, 4)
	}()

	pdu1, err := ReadPDU(client)
	require.NoError(t, err)
	pdu2, err := ReadPDU(client)
	require.NoError(t, err)
	require.NoError(t, <-done)

	// First PDU: offset=0
	offset1 := binary.BigEndian.Uint32(pdu1.BHS[40:44])
	assert.Equal(t, uint32(0), offset1)

	// Second PDU: offset=4
	offset2 := binary.BigEndian.Uint32(pdu2.BHS[40:44])
	assert.Equal(t, uint32(4), offset2)
}

func TestSendDataInZeroLength(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		done <- sendDataIn(server, 1, 0, []byte{}, 1, 0, 31, 65536)
	}()

	// Should send one PDU with empty data
	pdu, err := ReadPDU(client)
	require.NoError(t, err)
	require.NoError(t, <-done)

	assert.Equal(t, OpcodeDataIn, pdu.Opcode())
	// F bit set
	assert.Equal(t, byte(0x80), pdu.Flags()&0x80)
}
