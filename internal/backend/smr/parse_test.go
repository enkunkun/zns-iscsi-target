package smr

import (
	"encoding/binary"
	"testing"

	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildZoneDescriptorFixture builds a known 64-byte zone descriptor binary.
func buildZoneDescriptorFixture(zoneType zbc.ZoneType, condition zbc.ZoneCondition,
	zoneLen, startLBA, writePointer uint64, reset, nonSeq bool) []byte {
	data := make([]byte, zbc.ZoneDescriptorSize)
	byte0 := uint8(zoneType) | (uint8(condition) << 4)
	data[0] = byte0
	byte1 := uint8(0)
	if reset {
		byte1 |= 0x80
	}
	if nonSeq {
		byte1 |= 0x40
	}
	data[1] = byte1
	binary.BigEndian.PutUint64(data[8:16], zoneLen)
	binary.BigEndian.PutUint64(data[16:24], startLBA)
	binary.BigEndian.PutUint64(data[24:32], writePointer)
	return data
}

func TestParseZoneDescriptorEmpty(t *testing.T) {
	fixture := buildZoneDescriptorFixture(
		zbc.ZoneTypeSequentialWrite,
		zbc.ZoneConditionEmpty,
		524288, // 256MB in 512-byte sectors
		0,      // startLBA
		0,      // writePointer = startLBA (empty zone)
		false, false,
	)

	desc, err := parseZoneDescriptor(fixture)
	require.NoError(t, err)

	assert.Equal(t, zbc.ZoneTypeSequentialWrite, desc.ZoneType)
	assert.Equal(t, zbc.ZoneStateEmpty, desc.ZoneState)
	assert.Equal(t, zbc.ZoneConditionEmpty, desc.ZoneCondition)
	assert.Equal(t, uint64(524288), desc.ZoneLength)
	assert.Equal(t, uint64(0), desc.ZoneStartLBA)
	assert.Equal(t, uint64(0), desc.WritePointer)
	assert.False(t, desc.Reset)
	assert.False(t, desc.NonSeq)
}

func TestParseZoneDescriptorImplicitOpen(t *testing.T) {
	fixture := buildZoneDescriptorFixture(
		zbc.ZoneTypeSequentialWrite,
		zbc.ZoneConditionImplicitOpen,
		524288,
		524288,      // zone starts at sector 524288 (zone 1)
		524288+1000, // write pointer 1000 sectors into zone
		false, false,
	)

	desc, err := parseZoneDescriptor(fixture)
	require.NoError(t, err)

	assert.Equal(t, zbc.ZoneStateImplicitOpen, desc.ZoneState)
	assert.Equal(t, uint64(524288), desc.ZoneStartLBA)
	assert.Equal(t, uint64(524288+1000), desc.WritePointer)
}

func TestParseZoneDescriptorFull(t *testing.T) {
	fixture := buildZoneDescriptorFixture(
		zbc.ZoneTypeSequentialWrite,
		zbc.ZoneConditionFull,
		524288,
		0,
		524288, // write pointer at end (full)
		false, false,
	)

	desc, err := parseZoneDescriptor(fixture)
	require.NoError(t, err)

	assert.Equal(t, zbc.ZoneStateFull, desc.ZoneState)
	assert.Equal(t, uint64(524288), desc.WritePointer)
}

func TestParseZoneDescriptorReset(t *testing.T) {
	fixture := buildZoneDescriptorFixture(
		zbc.ZoneTypeSequentialWrite,
		zbc.ZoneConditionEmpty,
		524288, 0, 0,
		true, // reset recommended
		false,
	)

	desc, err := parseZoneDescriptor(fixture)
	require.NoError(t, err)
	assert.True(t, desc.Reset)
}

func TestParseZoneDescriptorTooShort(t *testing.T) {
	_, err := parseZoneDescriptor(make([]byte, 32)) // too short
	assert.Error(t, err)
}

func TestAllZoneConditions(t *testing.T) {
	tests := []struct {
		condition zbc.ZoneCondition
		state     zbc.ZoneState
	}{
		{zbc.ZoneConditionNotWritePointer, zbc.ZoneStateNotWritePointer},
		{zbc.ZoneConditionEmpty, zbc.ZoneStateEmpty},
		{zbc.ZoneConditionImplicitOpen, zbc.ZoneStateImplicitOpen},
		{zbc.ZoneConditionExplicitOpen, zbc.ZoneStateExplicitOpen},
		{zbc.ZoneConditionClosed, zbc.ZoneStateClosed},
		{zbc.ZoneConditionReadOnly, zbc.ZoneStateReadOnly},
		{zbc.ZoneConditionFull, zbc.ZoneStateFull},
		{zbc.ZoneConditionOffline, zbc.ZoneStateOffline},
	}
	for _, tt := range tests {
		t.Run(tt.condition.String(), func(t *testing.T) {
			fixture := buildZoneDescriptorFixture(
				zbc.ZoneTypeSequentialWrite, tt.condition,
				524288, 0, 0, false, false,
			)
			desc, err := parseZoneDescriptor(fixture)
			require.NoError(t, err)
			assert.Equal(t, tt.state, desc.ZoneState)
		})
	}
}

func TestParseReportZonesResponse(t *testing.T) {
	const headerSize = zbc.ReportZonesHeaderSize
	const descSize = zbc.ZoneDescriptorSize

	// Build a response with 2 zones
	buf := make([]byte, headerSize+2*descSize)
	binary.BigEndian.PutUint32(buf[0:4], uint32(2*descSize)) // zone list length

	zone0 := buildZoneDescriptorFixture(
		zbc.ZoneTypeSequentialWrite, zbc.ZoneConditionEmpty,
		524288, 0, 0, false, false,
	)
	copy(buf[headerSize:], zone0)

	zone1 := buildZoneDescriptorFixture(
		zbc.ZoneTypeSequentialWrite, zbc.ZoneConditionFull,
		524288, 524288, 524288*2, false, false,
	)
	copy(buf[headerSize+descSize:], zone1)

	zones, err := parseReportZonesResponse(buf)
	require.NoError(t, err)
	require.Len(t, zones, 2)

	assert.Equal(t, zbc.ZoneStateEmpty, zones[0].ZoneState)
	assert.Equal(t, uint64(0), zones[0].ZoneStartLBA)
	assert.Equal(t, zbc.ZoneStateFull, zones[1].ZoneState)
	assert.Equal(t, uint64(524288), zones[1].ZoneStartLBA)
}

func TestParseReportZonesResponseEmpty(t *testing.T) {
	buf := make([]byte, zbc.ReportZonesHeaderSize)
	binary.BigEndian.PutUint32(buf[0:4], 0) // no zones

	zones, err := parseReportZonesResponse(buf)
	require.NoError(t, err)
	assert.Nil(t, zones)
}

func TestParseReportZonesResponseTooShort(t *testing.T) {
	_, err := parseReportZonesResponse(make([]byte, 4)) // too short for header
	assert.Error(t, err)
}
