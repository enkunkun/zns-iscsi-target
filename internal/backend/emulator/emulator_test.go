package emulator

import (
	"testing"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEmulator(t *testing.T) *Emulator {
	t.Helper()
	e, err := New(Config{
		ZoneCount:    8,
		ZoneSizeMB:   1, // 1MB = 2048 sectors per zone (small for testing)
		MaxOpenZones: 3,
	})
	require.NoError(t, err)
	return e
}

func TestNewEmulator(t *testing.T) {
	e := newTestEmulator(t)
	assert.Equal(t, 8, e.ZoneCount())
	assert.Equal(t, uint64(2048), e.ZoneSize()) // 1MB / 512
	assert.Equal(t, 3, e.MaxOpenZones())
	assert.Equal(t, uint32(512), e.BlockSize())
	assert.Equal(t, uint64(8*2048), e.Capacity())
}

func TestNewEmulatorInvalidConfig(t *testing.T) {
	_, err := New(Config{ZoneCount: 0, ZoneSizeMB: 1, MaxOpenZones: 3})
	assert.Error(t, err)
	_, err = New(Config{ZoneCount: 4, ZoneSizeMB: 0, MaxOpenZones: 3})
	assert.Error(t, err)
	_, err = New(Config{ZoneCount: 4, ZoneSizeMB: 1, MaxOpenZones: 0})
	assert.Error(t, err)
}

func TestReportZones(t *testing.T) {
	e := newTestEmulator(t)
	zones, err := e.ReportZones(0, 0)
	require.NoError(t, err)
	assert.Len(t, zones, 8)
	assert.Equal(t, zbc.ZoneTypeSequentialWrite, zones[0].ZoneType)
	assert.Equal(t, zbc.ZoneStateEmpty, zones[0].ZoneState)
	assert.Equal(t, uint64(0), zones[0].ZoneStartLBA)
	assert.Equal(t, uint64(2048), zones[0].ZoneLength)
}

func TestReportZonesPartial(t *testing.T) {
	e := newTestEmulator(t)
	zones, err := e.ReportZones(0, 3)
	require.NoError(t, err)
	assert.Len(t, zones, 3)
}

func TestReportZonesFromMidpoint(t *testing.T) {
	e := newTestEmulator(t)
	// Zone 2 starts at sector 2*2048 = 4096
	zones, err := e.ReportZones(4096, 2)
	require.NoError(t, err)
	assert.Len(t, zones, 2)
	assert.Equal(t, uint64(4096), zones[0].ZoneStartLBA)
}

func TestSequentialWriteEnforcement(t *testing.T) {
	e := newTestEmulator(t)
	sector := make([]byte, 512)

	// Write to zone 0 at the write pointer (LBA=0)
	err := e.WriteSectors(0, sector)
	require.NoError(t, err)

	// Try writing at wrong LBA (LBA=0 again) - should fail
	err = e.WriteSectors(0, sector)
	assert.ErrorIs(t, err, backend.ErrOutOfOrder)

	// Write at correct new pointer (LBA=1)
	err = e.WriteSectors(1, sector)
	require.NoError(t, err)
}

func TestZoneStateMachineImplicitOpen(t *testing.T) {
	e := newTestEmulator(t)
	sector := make([]byte, 512)

	// Zone starts EMPTY
	zones, _ := e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateEmpty, zones[0].ZoneState)

	// Writing implicitly opens the zone
	err := e.WriteSectors(0, sector)
	require.NoError(t, err)

	zones, _ = e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateImplicitOpen, zones[0].ZoneState)
}

func TestZoneStateMachineFull(t *testing.T) {
	e := newTestEmulator(t)

	// Zone 0: 2048 sectors. Write all of them.
	zoneSize := int(e.ZoneSize())
	data := make([]byte, 512*zoneSize)
	err := e.WriteSectors(0, data)
	require.NoError(t, err)

	zones, _ := e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateFull, zones[0].ZoneState)

	// Write to full zone should fail
	err = e.WriteSectors(uint64(zoneSize), make([]byte, 512))
	// zone 0 is full; the next write after the zone end is zone 1
	require.NoError(t, err) // writing to zone 1 is OK
}

func TestZoneFullWriteError(t *testing.T) {
	e := newTestEmulator(t)

	// Fill zone 0
	zoneSize := int(e.ZoneSize())
	data := make([]byte, 512*zoneSize)
	require.NoError(t, e.WriteSectors(0, data))

	// Try writing at zone 0's start after it's full - should give full error
	err := e.WriteSectors(0, make([]byte, 512))
	assert.ErrorIs(t, err, backend.ErrOutOfOrder)
}

func TestResetZone(t *testing.T) {
	e := newTestEmulator(t)
	sector := make([]byte, 512)

	// Write something to zone 0
	require.NoError(t, e.WriteSectors(0, sector))

	zones, _ := e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateImplicitOpen, zones[0].ZoneState)
	assert.Equal(t, uint64(1), zones[0].WritePointer)

	// Reset the zone
	require.NoError(t, e.ResetZone(0))

	zones, _ = e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateEmpty, zones[0].ZoneState)
	assert.Equal(t, uint64(0), zones[0].WritePointer)
}

func TestMaxOpenZonesLimit(t *testing.T) {
	e := newTestEmulator(t)
	// MaxOpenZones = 3
	sector := make([]byte, 512)

	zoneSize := e.ZoneSize()

	// Open 3 zones (implicit open via write)
	for i := 0; i < 3; i++ {
		lba := uint64(i) * zoneSize
		require.NoError(t, e.WriteSectors(lba, sector), "zone %d", i)
	}

	// 4th open should fail (implicit open via write)
	lba := uint64(3) * zoneSize
	err := e.WriteSectors(lba, sector)
	assert.ErrorIs(t, err, backend.ErrTooManyOpenZones)
}

func TestExplicitOpenClose(t *testing.T) {
	e := newTestEmulator(t)
	zoneSize := e.ZoneSize()

	// Explicit open zone 0
	require.NoError(t, e.OpenZone(0))
	zones, _ := e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateExplicitOpen, zones[0].ZoneState)

	// Close zone 0
	require.NoError(t, e.CloseZone(0))
	zones, _ = e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateClosed, zones[0].ZoneState)

	// Now re-open via write
	sector := make([]byte, 512)
	require.NoError(t, e.WriteSectors(0, sector))

	// Closing a closed zone that becomes open again
	require.NoError(t, e.CloseZone(0))
	zones, _ = e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateClosed, zones[0].ZoneState)

	// Write to zone 1 to test cross-zone
	require.NoError(t, e.WriteSectors(zoneSize, sector))
}

func TestReadWrite(t *testing.T) {
	e := newTestEmulator(t)
	data := make([]byte, 512*4)
	for i := range data {
		data[i] = byte(i % 256)
	}

	require.NoError(t, e.WriteSectors(0, data))

	result, err := e.ReadSectors(0, 4)
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestReadUnwritten(t *testing.T) {
	e := newTestEmulator(t)
	// Reading unwritten data should return zeros
	result, err := e.ReadSectors(100, 4)
	require.NoError(t, err)
	assert.Len(t, result, 4*512)
	for _, b := range result {
		assert.Equal(t, byte(0), b)
	}
}

func TestCrossZoneRead(t *testing.T) {
	e := newTestEmulator(t)
	zoneSize := e.ZoneSize()

	// Write distinct data to zone 0 and zone 1
	zone0Data := make([]byte, 512)
	for i := range zone0Data {
		zone0Data[i] = 0xAA
	}
	zone1Data := make([]byte, 512)
	for i := range zone1Data {
		zone1Data[i] = 0xBB
	}

	// Fill zone 0 completely so zone 1 can be written
	bigData := make([]byte, int(zoneSize)*512-512) // fill all but last sector
	require.NoError(t, e.WriteSectors(0, bigData))
	require.NoError(t, e.WriteSectors(zoneSize-1, zone0Data))

	// Now write to zone 1
	require.NoError(t, e.WriteSectors(zoneSize, zone1Data))

	// Read last sector of zone 0 and first of zone 1
	result, err := e.ReadSectors(zoneSize-1, 2)
	require.NoError(t, err)
	assert.Equal(t, zone0Data, result[:512])
	assert.Equal(t, zone1Data, result[512:])
}

func TestFinishZone(t *testing.T) {
	e := newTestEmulator(t)

	require.NoError(t, e.OpenZone(0))
	zones, _ := e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateExplicitOpen, zones[0].ZoneState)

	require.NoError(t, e.FinishZone(0))
	zones, _ = e.ReportZones(0, 1)
	assert.Equal(t, zbc.ZoneStateFull, zones[0].ZoneState)
	assert.Equal(t, e.ZoneSize(), zones[0].WritePointer) // wp at end
}

func TestReadOutOfRange(t *testing.T) {
	e := newTestEmulator(t)
	_, err := e.ReadSectors(e.Capacity(), 1)
	assert.ErrorIs(t, err, backend.ErrInvalidLBA)
}

func TestOpenZoneMaxLimit(t *testing.T) {
	e := newTestEmulator(t)
	zoneSize := e.ZoneSize()

	// Explicitly open max zones
	for i := 0; i < 3; i++ {
		require.NoError(t, e.OpenZone(uint64(i)*zoneSize), "zone %d", i)
	}

	// 4th open should fail
	err := e.OpenZone(uint64(3) * zoneSize)
	assert.ErrorIs(t, err, backend.ErrTooManyOpenZones)
}
