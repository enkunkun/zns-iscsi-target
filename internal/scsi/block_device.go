package scsi

// BlockDevice is the interface that the SCSI handler uses to perform I/O.
// The ZTL struct satisfies this interface.
type BlockDevice interface {
	// Read reads sectorCount 512-byte sectors starting at lba.
	// Returns a byte slice of length sectorCount * 512.
	Read(lba uint64, sectorCount uint32) ([]byte, error)

	// Write writes data to the device starting at lba.
	// len(data) must be a multiple of 512.
	Write(lba uint64, data []byte) error

	// Flush flushes all pending write buffers to stable storage.
	Flush() error

	// Unmap marks the LBA range [lba, lba+sectorCount) as unmapped (TRIM).
	Unmap(lba uint64, sectorCount uint32) error

	// BlockSize returns the logical block size in bytes (always 512 in this target).
	BlockSize() uint32

	// Capacity returns the total number of logical blocks (sectors).
	Capacity() uint64
}
