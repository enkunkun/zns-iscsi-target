package ztl

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
)

// zoneBuffer holds buffered data for a single zone.
type zoneBuffer struct {
	zoneID       uint32
	data         []byte   // contiguous buffer for the zone
	writePointer uint64   // LBA where buffered data starts
	lastWriteAt  time.Time
}

// WriteBuffer accumulates writes per zone before flushing to the device.
type WriteBuffer struct {
	mu          sync.RWMutex
	buffers     map[uint32]*zoneBuffer // keyed by zone ID
	zoneSectors uint64                 // sectors per zone
	flushAge    time.Duration
	sectorSize  uint32
}

// NewWriteBuffer creates a new WriteBuffer.
func NewWriteBuffer(zoneSectors uint64, flushAgeSec int) *WriteBuffer {
	return &WriteBuffer{
		buffers:     make(map[uint32]*zoneBuffer),
		zoneSectors: zoneSectors,
		flushAge:    time.Duration(flushAgeSec) * time.Second,
		sectorSize:  512,
	}
}

// Add appends data to the zone's buffer.
// writePointer is the absolute LBA at which data should be written.
func (wb *WriteBuffer) Add(zoneID uint32, data []byte, writePointer uint64) error {
	if len(data) == 0 {
		return nil
	}

	wb.mu.Lock()
	defer wb.mu.Unlock()

	buf, ok := wb.buffers[zoneID]
	if !ok {
		buf = &zoneBuffer{
			zoneID:       zoneID,
			writePointer: writePointer,
		}
		wb.buffers[zoneID] = buf
	}

	buf.data = append(buf.data, data...)
	buf.lastWriteAt = time.Now()
	return nil
}

// Lookup checks if data at the given physical location is in the buffer.
// offsetSectors is the sector offset within the zone.
// length is the number of sectors to read.
// Returns the data and true if found (even partial), or nil and false.
func (wb *WriteBuffer) Lookup(zoneID uint32, startLBA uint64, sectorCount uint32) ([]byte, bool) {
	wb.mu.RLock()
	buf, ok := wb.buffers[zoneID]
	wb.mu.RUnlock()

	if !ok {
		return nil, false
	}

	// Check if the requested range is within the buffer
	bufStartLBA := buf.writePointer
	bufEndLBA := bufStartLBA + uint64(len(buf.data))/uint64(wb.sectorSize)

	requestStart := startLBA
	requestEnd := startLBA + uint64(sectorCount)

	if requestStart >= bufEndLBA || requestEnd <= bufStartLBA {
		return nil, false
	}

	// Calculate the overlap
	overlapStart := requestStart
	if bufStartLBA > overlapStart {
		overlapStart = bufStartLBA
	}
	overlapEnd := requestEnd
	if bufEndLBA < overlapEnd {
		overlapEnd = bufEndLBA
	}

	if overlapStart >= overlapEnd {
		return nil, false
	}

	// Only return if we have complete coverage
	if overlapStart != requestStart || overlapEnd != requestEnd {
		return nil, false
	}

	// Extract data from buffer
	bufOffset := int(overlapStart-bufStartLBA) * int(wb.sectorSize)
	length := int(overlapEnd-overlapStart) * int(wb.sectorSize)
	result := make([]byte, length)
	copy(result, buf.data[bufOffset:bufOffset+length])
	return result, true
}

// Flush writes all buffered data for a zone to the device sequentially.
func (wb *WriteBuffer) Flush(zoneID uint32, device backend.ZonedDevice, zm *ZoneManager) error {
	wb.mu.Lock()
	buf, ok := wb.buffers[zoneID]
	if !ok {
		wb.mu.Unlock()
		return nil
	}
	// Take ownership of the buffer entry and remove from map
	delete(wb.buffers, zoneID)
	wb.mu.Unlock()

	if len(buf.data) == 0 {
		return nil
	}

	// Ensure zone is open before writing
	if err := zm.GetOrOpen(zoneID); err != nil {
		return fmt.Errorf("WriteBuffer.Flush: open zone %d: %w", zoneID, err)
	}

	// Write sequentially to device
	if err := device.WriteSectors(buf.writePointer, buf.data); err != nil {
		return fmt.Errorf("WriteBuffer.Flush: write zone %d at lba %d: %w", zoneID, buf.writePointer, err)
	}

	// Update zone manager write pointer
	newWP := buf.writePointer + uint64(len(buf.data))/uint64(wb.sectorSize)
	zm.UpdateWritePointer(zoneID, newWP)

	return nil
}

// FlushAll flushes all buffered zones.
func (wb *WriteBuffer) FlushAll(device backend.ZonedDevice, zm *ZoneManager) error {
	wb.mu.Lock()
	zoneIDs := make([]uint32, 0, len(wb.buffers))
	for id := range wb.buffers {
		zoneIDs = append(zoneIDs, id)
	}
	wb.mu.Unlock()

	var firstErr error
	for _, id := range zoneIDs {
		if err := wb.Flush(id, device, zm); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// StartFlushLoop starts a background goroutine that flushes aged-out zone buffers.
func (wb *WriteBuffer) StartFlushLoop(ctx context.Context, device backend.ZonedDevice, zm *ZoneManager) {
	go func() {
		ticker := time.NewTicker(wb.flushAge / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				wb.flushAged(device, zm)
			}
		}
	}()
}

// flushAged flushes zones whose buffers are older than flushAge.
func (wb *WriteBuffer) flushAged(device backend.ZonedDevice, zm *ZoneManager) {
	threshold := time.Now().Add(-wb.flushAge)

	wb.mu.RLock()
	var aged []uint32
	for id, buf := range wb.buffers {
		if buf.lastWriteAt.Before(threshold) {
			aged = append(aged, id)
		}
	}
	wb.mu.RUnlock()

	for _, id := range aged {
		_ = wb.Flush(id, device, zm) // best effort
	}
}

// DirtyBytes returns the total number of unflushed bytes across all zones.
func (wb *WriteBuffer) DirtyBytes() int64 {
	wb.mu.RLock()
	defer wb.mu.RUnlock()
	var total int64
	for _, buf := range wb.buffers {
		total += int64(len(buf.data))
	}
	return total
}

// ZoneCount returns the number of zones with buffered data.
func (wb *WriteBuffer) ZoneCount() int {
	wb.mu.RLock()
	n := len(wb.buffers)
	wb.mu.RUnlock()
	return n
}
