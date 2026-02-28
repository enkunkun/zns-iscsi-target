// Package backend defines the ZonedDevice interface for zoned block devices.
package backend

import (
	"errors"

	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

// Common errors returned by ZonedDevice implementations.
var (
	// ErrOutOfOrder is returned when a write is not at the zone's write pointer.
	ErrOutOfOrder = errors.New("out of order write: LBA does not match write pointer")

	// ErrTooManyOpenZones is returned when the max open zones limit is reached.
	ErrTooManyOpenZones = errors.New("too many open zones")

	// ErrZoneFull is returned when writing to a full zone.
	ErrZoneFull = errors.New("zone is full")

	// ErrZoneOffline is returned when operating on an offline zone.
	ErrZoneOffline = errors.New("zone is offline")

	// ErrZoneReadOnly is returned when writing to a read-only zone.
	ErrZoneReadOnly = errors.New("zone is read-only")

	// ErrInvalidLBA is returned when the LBA is out of range.
	ErrInvalidLBA = errors.New("invalid LBA: out of device range")

	// ErrAlignmentError is returned when the LBA or count is not aligned to sector size.
	ErrAlignmentError = errors.New("alignment error: LBA or count not aligned")
)

// ZonedDevice is the interface for zoned block device backends.
// All LBAs are in units of 512-byte sectors.
type ZonedDevice interface {
	// ReportZones returns zone descriptors starting from startLBA.
	// If count == 0, all zones from startLBA are returned.
	ReportZones(startLBA uint64, count int) ([]zbc.ZoneDescriptor, error)

	// ZoneCount returns the total number of zones on the device.
	ZoneCount() int

	// ZoneSize returns the size of a single zone in sectors.
	ZoneSize() uint64

	// OpenZone sends an explicit OPEN ZONE command.
	OpenZone(zoneStartLBA uint64) error

	// CloseZone sends a CLOSE ZONE command.
	CloseZone(zoneStartLBA uint64) error

	// FinishZone sends a FINISH ZONE command, transitioning zone to FULL.
	FinishZone(zoneStartLBA uint64) error

	// ResetZone resets the write pointer of a zone to the zone start.
	ResetZone(zoneStartLBA uint64) error

	// ReadSectors reads sectors from the device.
	// lba is the starting sector, count is the number of sectors to read.
	// Returns a byte slice of len(count * BlockSize()).
	ReadSectors(lba uint64, count uint32) ([]byte, error)

	// WriteSectors writes sectors to a zone at the write pointer position.
	// lba must equal the zone's current write pointer.
	// data length must be a multiple of BlockSize().
	WriteSectors(lba uint64, data []byte) error

	// BlockSize returns the logical block size in bytes (always 512).
	BlockSize() uint32

	// Capacity returns the total device capacity in sectors.
	Capacity() uint64

	// MaxOpenZones returns the maximum number of simultaneously open zones.
	MaxOpenZones() int

	// Close releases all resources associated with the device.
	Close() error
}
