package smr

import (
	"encoding/binary"

	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

const (
	// defaultTimeout is the SCSI command timeout in milliseconds.
	defaultTimeout = 30000

	// reportZonesMaxLen is the maximum allocation length for REPORT ZONES.
	reportZonesMaxLen = 512 * 1024
)

// buildReportZonesCDB builds the REPORT ZONES CDB (opcode 0x95).
func buildReportZonesCDB(startLBA uint64, allocLen uint32, reportingOptions uint8) []byte {
	cdb := make([]byte, 16)
	cdb[0] = zbc.OpcodeReportZones
	cdb[1] = 0x00
	binary.BigEndian.PutUint64(cdb[2:10], startLBA)
	binary.BigEndian.PutUint32(cdb[10:14], allocLen)
	cdb[14] = reportingOptions
	cdb[15] = 0x00
	return cdb
}

// buildZoneActionCDB builds the ZONE ACTION CDB (opcode 0x9F).
func buildZoneActionCDB(action uint8, startLBA uint64, all bool) []byte {
	cdb := make([]byte, 16)
	cdb[0] = zbc.OpcodeZoneAction
	cdb[1] = action
	binary.BigEndian.PutUint64(cdb[2:10], startLBA)
	// bytes 10-13: reserved
	if all {
		cdb[14] = 0x01
	}
	cdb[15] = 0x00
	return cdb
}

// buildRead16CDB builds a SCSI READ(16) CDB.
func buildRead16CDB(lba uint64, count uint32) []byte {
	cdb := make([]byte, 16)
	cdb[0] = 0x88 // READ(16)
	binary.BigEndian.PutUint64(cdb[2:10], lba)
	binary.BigEndian.PutUint32(cdb[10:14], count)
	return cdb
}

// buildWrite16CDB builds a SCSI WRITE(16) CDB.
func buildWrite16CDB(lba uint64, count uint32) []byte {
	cdb := make([]byte, 16)
	cdb[0] = 0x8A // WRITE(16)
	binary.BigEndian.PutUint64(cdb[2:10], lba)
	binary.BigEndian.PutUint32(cdb[10:14], count)
	return cdb
}
