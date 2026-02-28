// Package emulator provides an in-memory zoned block device emulator.
package emulator

import (
	"fmt"
	"sync"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

// zoneState holds the internal state for a single zone.
type zoneState struct {
	state        zbc.ZoneState
	writePointer uint64 // absolute LBA of write pointer
	data         []byte // zone data buffer
}

// Emulator is an in-memory zoned block device emulator.
type Emulator struct {
	mu           sync.RWMutex
	zones        []zoneState
	zoneCount    int
	zoneSectors  uint64 // sectors per zone
	maxOpenZones int
	openCount    int
}

// Config holds emulator configuration.
type Config struct {
	ZoneCount    int
	ZoneSizeMB   int
	MaxOpenZones int
}

// New creates a new in-memory Emulator with the given configuration.
func New(cfg Config) (*Emulator, error) {
	if cfg.ZoneCount <= 0 {
		return nil, fmt.Errorf("zone_count must be positive, got %d", cfg.ZoneCount)
	}
	if cfg.ZoneSizeMB <= 0 {
		return nil, fmt.Errorf("zone_size_mb must be positive, got %d", cfg.ZoneSizeMB)
	}
	if cfg.MaxOpenZones <= 0 {
		return nil, fmt.Errorf("max_open_zones must be positive, got %d", cfg.MaxOpenZones)
	}

	const sectorSize = 512
	zoneSectors := uint64(cfg.ZoneSizeMB) * 1024 * 1024 / sectorSize
	zones := make([]zoneState, cfg.ZoneCount)

	for i := range zones {
		startLBA := uint64(i) * zoneSectors
		zones[i] = zoneState{
			state:        zbc.ZoneStateEmpty,
			writePointer: startLBA,
			data:         make([]byte, int(zoneSectors)*sectorSize),
		}
	}

	return &Emulator{
		zones:        zones,
		zoneCount:    cfg.ZoneCount,
		zoneSectors:  zoneSectors,
		maxOpenZones: cfg.MaxOpenZones,
	}, nil
}

// zoneIndex returns the zone index for a given LBA.
func (e *Emulator) zoneIndex(lba uint64) (int, error) {
	idx := int(lba / e.zoneSectors)
	if idx >= e.zoneCount {
		return 0, fmt.Errorf("%w: lba=%d capacity=%d", backend.ErrInvalidLBA, lba, e.Capacity())
	}
	return idx, nil
}

// zoneStartLBA returns the start LBA of zone at index idx.
func (e *Emulator) zoneStartLBA(idx int) uint64 {
	return uint64(idx) * e.zoneSectors
}

// isOpen returns true if the zone state represents an open zone.
func isOpen(s zbc.ZoneState) bool {
	return s == zbc.ZoneStateImplicitOpen || s == zbc.ZoneStateExplicitOpen
}

// ReportZones returns zone descriptors starting from startLBA.
func (e *Emulator) ReportZones(startLBA uint64, count int) ([]zbc.ZoneDescriptor, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	startIdx := int(startLBA / e.zoneSectors)
	if startIdx >= e.zoneCount {
		return nil, fmt.Errorf("%w: startLBA=%d", backend.ErrInvalidLBA, startLBA)
	}

	end := e.zoneCount
	if count > 0 && startIdx+count < end {
		end = startIdx + count
	}

	descs := make([]zbc.ZoneDescriptor, 0, end-startIdx)
	for i := startIdx; i < end; i++ {
		z := &e.zones[i]
		startLBAi := e.zoneStartLBA(i)
		desc := zbc.ZoneDescriptor{
			ZoneType:     zbc.ZoneTypeSequentialWrite,
			ZoneState:    z.state,
			ZoneLength:   e.zoneSectors,
			ZoneStartLBA: startLBAi,
			WritePointer: z.writePointer,
		}
		descs = append(descs, desc)
	}
	return descs, nil
}

// ZoneCount returns the total number of zones.
func (e *Emulator) ZoneCount() int {
	return e.zoneCount
}

// ZoneSize returns the size of a zone in sectors.
func (e *Emulator) ZoneSize() uint64 {
	return e.zoneSectors
}

// openZoneLocked opens a zone, enforcing the max open zones limit.
// Must be called with e.mu held (write lock).
func (e *Emulator) openZoneLocked(idx int, explicit bool) error {
	z := &e.zones[idx]
	switch z.state {
	case zbc.ZoneStateExplicitOpen:
		// already explicitly open; nothing to do
		return nil
	case zbc.ZoneStateImplicitOpen:
		if explicit {
			z.state = zbc.ZoneStateExplicitOpen
		}
		return nil
	case zbc.ZoneStateEmpty, zbc.ZoneStateClosed:
		if e.openCount >= e.maxOpenZones {
			return fmt.Errorf("%w: limit=%d", backend.ErrTooManyOpenZones, e.maxOpenZones)
		}
		if explicit {
			z.state = zbc.ZoneStateExplicitOpen
		} else {
			z.state = zbc.ZoneStateImplicitOpen
		}
		e.openCount++
		return nil
	case zbc.ZoneStateFull:
		return fmt.Errorf("%w: zone %d is full", backend.ErrZoneFull, idx)
	default:
		return fmt.Errorf("cannot open zone %d in state %s", idx, z.state)
	}
}

// OpenZone explicitly opens a zone.
func (e *Emulator) OpenZone(zoneStartLBA uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx, err := e.zoneIndex(zoneStartLBA)
	if err != nil {
		return err
	}
	return e.openZoneLocked(idx, true)
}

// CloseZone closes an open zone.
func (e *Emulator) CloseZone(zoneStartLBA uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx, err := e.zoneIndex(zoneStartLBA)
	if err != nil {
		return err
	}
	z := &e.zones[idx]
	switch z.state {
	case zbc.ZoneStateImplicitOpen, zbc.ZoneStateExplicitOpen:
		z.state = zbc.ZoneStateClosed
		e.openCount--
	case zbc.ZoneStateClosed, zbc.ZoneStateEmpty:
		// No-op: already closed or empty
	default:
		return fmt.Errorf("cannot close zone %d in state %s", idx, z.state)
	}
	return nil
}

// FinishZone transitions a zone to FULL.
func (e *Emulator) FinishZone(zoneStartLBA uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx, err := e.zoneIndex(zoneStartLBA)
	if err != nil {
		return err
	}
	z := &e.zones[idx]
	switch z.state {
	case zbc.ZoneStateImplicitOpen, zbc.ZoneStateExplicitOpen:
		z.state = zbc.ZoneStateFull
		z.writePointer = e.zoneStartLBA(idx) + e.zoneSectors
		e.openCount--
	case zbc.ZoneStateClosed:
		z.state = zbc.ZoneStateFull
		z.writePointer = e.zoneStartLBA(idx) + e.zoneSectors
	case zbc.ZoneStateEmpty:
		z.state = zbc.ZoneStateFull
		z.writePointer = e.zoneStartLBA(idx) + e.zoneSectors
	case zbc.ZoneStateFull:
		// already full
	default:
		return fmt.Errorf("cannot finish zone %d in state %s", idx, z.state)
	}
	return nil
}

// ResetZone resets the write pointer of a zone to the zone start.
func (e *Emulator) ResetZone(zoneStartLBA uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	idx, err := e.zoneIndex(zoneStartLBA)
	if err != nil {
		return err
	}
	z := &e.zones[idx]
	wasOpen := isOpen(z.state)
	z.state = zbc.ZoneStateEmpty
	z.writePointer = e.zoneStartLBA(idx)
	// Clear zone data
	for i := range z.data {
		z.data[i] = 0
	}
	if wasOpen {
		e.openCount--
	}
	return nil
}

// ReadSectors reads sectors from the device.
func (e *Emulator) ReadSectors(lba uint64, count uint32) ([]byte, error) {
	if count == 0 {
		return []byte{}, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	const sectorSize = 512
	result := make([]byte, int(count)*sectorSize)
	readEnd := lba + uint64(count)

	if readEnd > e.Capacity() {
		return nil, fmt.Errorf("%w: lba=%d count=%d capacity=%d", backend.ErrInvalidLBA, lba, count, e.Capacity())
	}

	// Copy data across zone boundaries
	remaining := int(count) * sectorSize
	offset := 0
	currentLBA := lba

	for remaining > 0 {
		idx := int(currentLBA / e.zoneSectors)
		if idx >= e.zoneCount {
			break
		}
		z := &e.zones[idx]
		zoneStart := e.zoneStartLBA(idx)
		offsetInZone := int(currentLBA-zoneStart) * sectorSize
		available := len(z.data) - offsetInZone
		if available <= 0 {
			break
		}
		toCopy := remaining
		if toCopy > available {
			toCopy = available
		}
		copy(result[offset:], z.data[offsetInZone:offsetInZone+toCopy])
		offset += toCopy
		remaining -= toCopy
		currentLBA += uint64(toCopy / sectorSize)
	}

	return result, nil
}

// WriteSectors writes sectors to a zone at the write pointer position.
func (e *Emulator) WriteSectors(lba uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	const sectorSize = 512
	if len(data)%sectorSize != 0 {
		return fmt.Errorf("%w: data length %d not a multiple of sector size", backend.ErrAlignmentError, len(data))
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	idx, err := e.zoneIndex(lba)
	if err != nil {
		return err
	}
	z := &e.zones[idx]

	// Enforce sequential write: lba must equal write pointer
	if lba != z.writePointer {
		return fmt.Errorf("%w: lba=%d writePointer=%d", backend.ErrOutOfOrder, lba, z.writePointer)
	}

	// Check zone state
	switch z.state {
	case zbc.ZoneStateEmpty, zbc.ZoneStateClosed:
		// Implicit open
		if err := e.openZoneLocked(idx, false); err != nil {
			return err
		}
	case zbc.ZoneStateImplicitOpen, zbc.ZoneStateExplicitOpen:
		// OK
	case zbc.ZoneStateFull:
		return fmt.Errorf("%w: zone %d", backend.ErrZoneFull, idx)
	case zbc.ZoneStateReadOnly:
		return fmt.Errorf("%w: zone %d", backend.ErrZoneReadOnly, idx)
	case zbc.ZoneStateOffline:
		return fmt.Errorf("%w: zone %d", backend.ErrZoneOffline, idx)
	default:
		return fmt.Errorf("zone %d is in unexpected state %s", idx, z.state)
	}

	// Check write fits in the zone
	zoneStart := e.zoneStartLBA(idx)
	zoneEnd := zoneStart + e.zoneSectors
	writeEnd := lba + uint64(len(data)/sectorSize)
	if writeEnd > zoneEnd {
		return fmt.Errorf("write crosses zone boundary: lba=%d end=%d zone_end=%d", lba, writeEnd, zoneEnd)
	}

	// Write data into zone buffer
	offsetInZone := int(lba-zoneStart) * sectorSize
	copy(z.data[offsetInZone:], data)
	z.writePointer = writeEnd

	// Transition to FULL if write pointer is at zone end
	if z.writePointer == zoneEnd {
		z.state = zbc.ZoneStateFull
		e.openCount--
	}

	return nil
}

// BlockSize returns the logical block size (always 512).
func (e *Emulator) BlockSize() uint32 {
	return 512
}

// Capacity returns the total device capacity in sectors.
func (e *Emulator) Capacity() uint64 {
	return uint64(e.zoneCount) * e.zoneSectors
}

// MaxOpenZones returns the maximum simultaneously open zones.
func (e *Emulator) MaxOpenZones() int {
	return e.maxOpenZones
}

// Close releases all emulator resources.
func (e *Emulator) Close() error {
	return nil
}
