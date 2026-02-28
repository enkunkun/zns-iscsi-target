package smr

import (
	"encoding/binary"
	"testing"

	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
	"github.com/stretchr/testify/assert"
)

func TestBuildReportZonesCDB(t *testing.T) {
	cdb := buildReportZonesCDB(0x10000, 0x40000, zbc.ReportingAll)
	assert.Equal(t, byte(zbc.OpcodeReportZones), cdb[0])
	assert.Equal(t, uint64(0x10000), binary.BigEndian.Uint64(cdb[2:10]))
	assert.Equal(t, uint32(0x40000), binary.BigEndian.Uint32(cdb[10:14]))
	assert.Equal(t, byte(zbc.ReportingAll), cdb[14])
}

func TestBuildZoneActionCDB(t *testing.T) {
	// Reset zone at LBA 0, not all
	cdb := buildZoneActionCDB(zbc.ZoneActionReset, 0x1000, false)
	assert.Equal(t, byte(zbc.OpcodeZoneAction), cdb[0])
	assert.Equal(t, byte(zbc.ZoneActionReset), cdb[1])
	assert.Equal(t, uint64(0x1000), binary.BigEndian.Uint64(cdb[2:10]))
	assert.Equal(t, byte(0x00), cdb[14])

	// All zones
	cdb2 := buildZoneActionCDB(zbc.ZoneActionReset, 0, true)
	assert.Equal(t, byte(0x01), cdb2[14])
}

func TestBuildRead16CDB(t *testing.T) {
	cdb := buildRead16CDB(0x100, 8)
	assert.Equal(t, byte(0x88), cdb[0])
	assert.Equal(t, uint64(0x100), binary.BigEndian.Uint64(cdb[2:10]))
	assert.Equal(t, uint32(8), binary.BigEndian.Uint32(cdb[10:14]))
}

func TestBuildWrite16CDB(t *testing.T) {
	cdb := buildWrite16CDB(0x200, 16)
	assert.Equal(t, byte(0x8A), cdb[0])
	assert.Equal(t, uint64(0x200), binary.BigEndian.Uint64(cdb[2:10]))
	assert.Equal(t, uint32(16), binary.BigEndian.Uint32(cdb[10:14]))
}
