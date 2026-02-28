// Package ztl implements the Zone Translation Layer.
package ztl

// PhysAddr is an encoded 64-bit physical address.
// Bits 63-40: ZoneID (24 bits, up to 16M zones)
// Bits 39-0:  OffsetSectors (40 bits, up to 1T sectors per zone)
type PhysAddr uint64

const (
	zoneIDBits     = 24
	offsetBits     = 40
	offsetMask     = (uint64(1) << offsetBits) - 1
	zoneIDMask     = (uint64(1) << zoneIDBits) - 1
)

// EncodePhysAddr encodes a zone ID and sector offset into a PhysAddr.
func EncodePhysAddr(zoneID uint32, offsetSectors uint64) PhysAddr {
	return PhysAddr((uint64(zoneID)<<offsetBits) | (offsetSectors & offsetMask))
}

// ZoneID extracts the zone ID from a PhysAddr.
func (p PhysAddr) ZoneID() uint32 {
	return uint32((uint64(p) >> offsetBits) & zoneIDMask)
}

// OffsetSectors extracts the sector offset within the zone from a PhysAddr.
func (p PhysAddr) OffsetSectors() uint64 {
	return uint64(p) & offsetMask
}

// IsUnmapped returns true if the PhysAddr represents an unmapped segment (value 0).
func (p PhysAddr) IsUnmapped() bool {
	return uint64(p) == 0
}

// SegmentID is the logical segment index.
type SegmentID = uint64

// LBAToSegmentID converts a logical block address to a segment ID.
// sectorsPerSegment is the number of 512-byte sectors per segment.
func LBAToSegmentID(lba uint64, sectorsPerSegment uint64) SegmentID {
	return lba / sectorsPerSegment
}

// SegmentToLBARange returns the start and end LBA (exclusive) for a given segment ID.
func SegmentToLBARange(segID SegmentID, sectorsPerSegment uint64) (startLBA, endLBA uint64) {
	startLBA = segID * sectorsPerSegment
	endLBA = startLBA + sectorsPerSegment
	return
}

// SegmentOffsetSectors returns the sector offset within a segment.
func SegmentOffsetSectors(lba uint64, sectorsPerSegment uint64) uint64 {
	return lba % sectorsPerSegment
}
