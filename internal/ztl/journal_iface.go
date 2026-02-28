package ztl

// Journal is the interface that ZTL uses to log operations.
// The actual implementation is in internal/journal, but we define the
// interface here to avoid circular dependencies.
type Journal interface {
	// LogL2PUpdate logs an L2P mapping update.
	// Returns the Log Sequence Number assigned to this record.
	LogL2PUpdate(segID uint64, oldPhys, newPhys PhysAddr) (uint64, error)

	// LogZoneOpen logs a zone open operation.
	LogZoneOpen(zoneID uint32) error

	// LogZoneClose logs a zone close operation.
	LogZoneClose(zoneID uint32) error

	// LogZoneReset logs a zone reset intent.
	LogZoneReset(zoneID uint32) error

	// GroupCommit flushes pending log records to stable storage.
	GroupCommit() error
}
