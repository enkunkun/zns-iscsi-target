package iscsi

import (
	"encoding/binary"
	"fmt"
	"sync"
)

// dataOutBuffer holds the reassembly state for a single SCSI write command.
type dataOutBuffer struct {
	mu            sync.Mutex
	itt           uint32  // InitiatorTaskTag
	expectedLen   uint32  // Expected total data length
	data          []byte  // Accumulated data
	nextDataSN    uint32  // Expected next DataSN
}

// newDataOutBuffer creates a new reassembly buffer for a write command.
func newDataOutBuffer(itt, expectedLen uint32) *dataOutBuffer {
	return &dataOutBuffer{
		itt:         itt,
		expectedLen: expectedLen,
		data:        make([]byte, 0, expectedLen),
	}
}

// add appends data from a Data-Out PDU to the buffer.
// Returns true if this was the final segment (F bit set).
func (b *dataOutBuffer) add(pdu *PDU) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	finalBit := (pdu.BHS[1] & 0x80) != 0
	dataSN := binary.BigEndian.Uint32(pdu.BHS[36:40])
	bufferOffset := binary.BigEndian.Uint32(pdu.BHS[40:44])

	_ = dataSN // we accept out-of-order for simplicity; DataSN is used for logging

	// Ensure buffer is large enough
	needed := bufferOffset + uint32(len(pdu.DataSegment))
	if needed > uint32(cap(b.data)) {
		newData := make([]byte, max(int(needed), cap(b.data)*2))
		copy(newData, b.data)
		b.data = newData[:max(int(needed), len(b.data))]
	} else if needed > uint32(len(b.data)) {
		b.data = b.data[:needed]
	}

	copy(b.data[bufferOffset:], pdu.DataSegment)
	b.nextDataSN = dataSN + 1
	return finalBit, nil
}

// assembledData returns the complete data buffer.
func (b *dataOutBuffer) assembledData() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data
}

// ReassemblyMap manages Data-Out reassembly buffers keyed by ITT.
type ReassemblyMap struct {
	mu      sync.Mutex
	buffers map[uint32]*dataOutBuffer
}

// newReassemblyMap creates a new reassembly map.
func newReassemblyMap() *ReassemblyMap {
	return &ReassemblyMap{
		buffers: make(map[uint32]*dataOutBuffer),
	}
}

// initBuffer initializes a new reassembly buffer for the given ITT.
func (m *ReassemblyMap) initBuffer(itt, expectedLen uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buffers[itt] = newDataOutBuffer(itt, expectedLen)
}

// addDataOut adds a Data-Out PDU to the appropriate buffer.
// Returns (assembled, finalFlag, error).
func (m *ReassemblyMap) addDataOut(pdu *PDU) ([]byte, bool, error) {
	itt := pdu.InitiatorTaskTag()

	m.mu.Lock()
	buf, ok := m.buffers[itt]
	m.mu.Unlock()

	if !ok {
		return nil, false, fmt.Errorf("no reassembly buffer for ITT 0x%08X", itt)
	}

	final, err := buf.add(pdu)
	if err != nil {
		return nil, false, err
	}

	if final {
		m.mu.Lock()
		delete(m.buffers, itt)
		m.mu.Unlock()
		return buf.assembledData(), true, nil
	}
	return nil, false, nil
}

// clearBuffer removes the reassembly buffer for the given ITT (on error/abort).
func (m *ReassemblyMap) clearBuffer(itt uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.buffers, itt)
}
