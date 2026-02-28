package scsi

import "fmt"

// SYNCHRONIZE CACHE opcodes.
const (
	OpcodeSyncCache10 byte = 0x35
	OpcodeSyncCache16 byte = 0x91
)

// handleSyncCache10 processes a SYNCHRONIZE CACHE (10) CDB.
// Flushes all write buffers to stable storage.
func handleSyncCache10(cdb []byte, dev BlockDevice) error {
	if len(cdb) < 10 {
		return fmt.Errorf("SYNCHRONIZE CACHE(10) CDB too short: %d bytes", len(cdb))
	}
	return dev.Flush()
}

// handleSyncCache16 processes a SYNCHRONIZE CACHE (16) CDB.
// Flushes all write buffers to stable storage.
func handleSyncCache16(cdb []byte, dev BlockDevice) error {
	if len(cdb) < 16 {
		return fmt.Errorf("SYNCHRONIZE CACHE(16) CDB too short: %d bytes", len(cdb))
	}
	return dev.Flush()
}
