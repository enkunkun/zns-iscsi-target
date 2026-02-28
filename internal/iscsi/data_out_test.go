package iscsi

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildDataOutPDU(itt uint32, dataSN, bufferOffset uint32, finalBit bool, data []byte) *PDU {
	pdu := &PDU{}
	pdu.SetOpcode(OpcodeDataOut)
	if finalBit {
		pdu.BHS[1] = 0x80 // F bit
	}
	pdu.SetInitiatorTaskTag(itt)
	// DataSN at bytes 36-39
	binary.BigEndian.PutUint32(pdu.BHS[36:40], dataSN)
	// Buffer offset at bytes 40-43
	binary.BigEndian.PutUint32(pdu.BHS[40:44], bufferOffset)
	pdu.DataSegment = data
	return pdu
}

func TestReassemblyMapSinglePDU(t *testing.T) {
	m := newReassemblyMap()
	itt := uint32(0x1234)
	data := []byte("hello world - this is test data!")
	expectedLen := uint32(len(data))

	m.initBuffer(itt, expectedLen)

	pdu := buildDataOutPDU(itt, 0, 0, true, data)
	assembled, final, err := m.addDataOut(pdu)
	require.NoError(t, err)
	assert.True(t, final)
	assert.Equal(t, data, assembled)

	// Buffer should be removed after final
	_, _, err = m.addDataOut(pdu)
	assert.Error(t, err)
}

func TestReassemblyMapMultiplePDUs(t *testing.T) {
	m := newReassemblyMap()
	itt := uint32(0xABCD)

	chunk1 := []byte("first chunk of data - 512 bytes")
	chunk2 := []byte("second chunk of data - 512 bytes")
	totalLen := uint32(len(chunk1) + len(chunk2))

	m.initBuffer(itt, totalLen)

	// First PDU (not final)
	pdu1 := buildDataOutPDU(itt, 0, 0, false, chunk1)
	assembled1, final1, err := m.addDataOut(pdu1)
	require.NoError(t, err)
	assert.False(t, final1)
	assert.Nil(t, assembled1)

	// Second PDU (final)
	pdu2 := buildDataOutPDU(itt, 1, uint32(len(chunk1)), true, chunk2)
	assembled2, final2, err := m.addDataOut(pdu2)
	require.NoError(t, err)
	assert.True(t, final2)
	require.NotNil(t, assembled2)
	assert.Equal(t, append(chunk1, chunk2...), assembled2)
}

func TestReassemblyMapUnknownITT(t *testing.T) {
	m := newReassemblyMap()
	pdu := buildDataOutPDU(0x9999, 0, 0, true, []byte("data"))
	_, _, err := m.addDataOut(pdu)
	assert.Error(t, err)
}

func TestReassemblyMapClearBuffer(t *testing.T) {
	m := newReassemblyMap()
	itt := uint32(1)
	m.initBuffer(itt, 512)

	m.clearBuffer(itt)

	// Should fail since buffer was cleared
	pdu := buildDataOutPDU(itt, 0, 0, true, []byte("data"))
	_, _, err := m.addDataOut(pdu)
	assert.Error(t, err)
}

func TestReassemblyMapMultipleITTs(t *testing.T) {
	m := newReassemblyMap()

	itt1 := uint32(1)
	itt2 := uint32(2)
	data1 := []byte("data for ITT 1")
	data2 := []byte("data for ITT 2")

	m.initBuffer(itt1, uint32(len(data1)))
	m.initBuffer(itt2, uint32(len(data2)))

	pdu1 := buildDataOutPDU(itt1, 0, 0, true, data1)
	pdu2 := buildDataOutPDU(itt2, 0, 0, true, data2)

	assembled1, final1, err1 := m.addDataOut(pdu1)
	assembled2, final2, err2 := m.addDataOut(pdu2)

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.True(t, final1)
	assert.True(t, final2)
	assert.Equal(t, data1, assembled1)
	assert.Equal(t, data2, assembled2)
}
