package smr

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTransport implements ScsiTransport for testing without real hardware.
type mockTransport struct {
	zones  []zbc.ZoneDescriptor
	closed bool
	// pdt is the Peripheral Device Type returned by INQUIRY (default: 0x14).
	pdt uint8
	// zonedModel is the VPD B1 zoned field (0=non-zoned, 1=Host-Aware, 2=Host-Managed).
	zonedModel uint8
	// scsiReadFn allows tests to override ScsiRead behavior.
	scsiReadFn func(cdb []byte, buf []byte) error
	// scsiWriteFn allows tests to override ScsiWrite behavior.
	scsiWriteFn func(cdb []byte, buf []byte) error
	// scsiNoDataFn allows tests to override ScsiNoData behavior.
	scsiNoDataFn func(cdb []byte) error
}

func newMockTransport(zones []zbc.ZoneDescriptor) *mockTransport {
	m := &mockTransport{
		zones:      zones,
		pdt:        zbc.PeripheralDeviceTypeZBC, // default: Host-Managed
		zonedModel: 0x02,                        // default: Host-Managed
	}
	m.scsiReadFn = m.defaultScsiRead
	return m
}

func (m *mockTransport) ScsiRead(cdb []byte, buf []byte, _ uint32) error {
	return m.scsiReadFn(cdb, buf)
}

func (m *mockTransport) ScsiWrite(cdb []byte, buf []byte, _ uint32) error {
	if m.scsiWriteFn != nil {
		return m.scsiWriteFn(cdb, buf)
	}
	return nil
}

func (m *mockTransport) ScsiNoData(cdb []byte, _ uint32) error {
	if m.scsiNoDataFn != nil {
		return m.scsiNoDataFn(cdb)
	}
	return nil
}

func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

// defaultScsiRead handles REPORT ZONES CDB by returning mock zone data.
func (m *mockTransport) defaultScsiRead(cdb []byte, buf []byte) error {
	if len(cdb) < 1 {
		return errors.New("empty CDB")
	}

	switch cdb[0] {
	case zbc.OpcodeInquiry:
		return m.handleInquiry(cdb, buf)
	case zbc.OpcodeReportZones:
		return m.handleReportZones(cdb, buf)
	case 0x88: // READ(16)
		// Fill with deterministic test data
		for i := range buf {
			buf[i] = 0xAA
		}
		return nil
	default:
		return nil
	}
}

// handleInquiry builds a mock INQUIRY response.
func (m *mockTransport) handleInquiry(cdb []byte, buf []byte) error {
	evpd := cdb[1] & 0x01
	if evpd == 0 {
		// Standard INQUIRY — set PDT in byte 0
		if len(buf) > 0 {
			buf[0] = m.pdt
		}
		return nil
	}
	// VPD INQUIRY
	pageCode := cdb[2]
	if pageCode == zbc.VPDPageBlockDeviceChar {
		if len(buf) > 8 {
			buf[8] = m.zonedModel << 4
		}
	}
	return nil
}

// handleReportZones builds a mock REPORT ZONES response.
func (m *mockTransport) handleReportZones(_ []byte, buf []byte) error {
	const headerSize = zbc.ReportZonesHeaderSize
	const descSize = zbc.ZoneDescriptorSize

	numZones := len(m.zones)
	zoneListLen := uint32(numZones * descSize)
	binary.BigEndian.PutUint32(buf[0:4], zoneListLen)

	offset := headerSize
	for _, z := range m.zones {
		if offset+descSize > len(buf) {
			break
		}
		// Encode zone descriptor
		byte0 := uint8(z.ZoneType) | (uint8(z.ZoneCondition) << 4)
		buf[offset] = byte0
		var byte1 uint8
		if z.Reset {
			byte1 |= 0x80
		}
		if z.NonSeq {
			byte1 |= 0x40
		}
		buf[offset+1] = byte1
		binary.BigEndian.PutUint64(buf[offset+8:offset+16], z.ZoneLength)
		binary.BigEndian.PutUint64(buf[offset+16:offset+24], z.ZoneStartLBA)
		binary.BigEndian.PutUint64(buf[offset+24:offset+32], z.WritePointer)
		offset += descSize
	}

	return nil
}

func makeTestZones(count int, zoneSize uint64) []zbc.ZoneDescriptor {
	zones := make([]zbc.ZoneDescriptor, count)
	for i := range zones {
		zones[i] = zbc.ZoneDescriptor{
			ZoneType:      zbc.ZoneTypeSequentialWrite,
			ZoneState:     zbc.ZoneStateEmpty,
			ZoneCondition: zbc.ZoneConditionEmpty,
			ZoneLength:    zoneSize,
			ZoneStartLBA:  uint64(i) * zoneSize,
			WritePointer:  uint64(i) * zoneSize,
		}
	}
	return zones
}

func TestSMRBackendDiscover(t *testing.T) {
	zones := makeTestZones(4, 524288)
	transport := newMockTransport(zones)

	smrDev, err := newSMRBackend(transport)
	require.NoError(t, err)

	assert.Equal(t, 4, smrDev.ZoneCount())
	assert.Equal(t, uint64(524288), smrDev.ZoneSize())
	assert.Equal(t, uint64(4*524288), smrDev.Capacity())
	assert.Equal(t, 14, smrDev.MaxOpenZones())
	assert.Equal(t, uint32(512), smrDev.BlockSize())
}

func TestSMRBackendReportZones(t *testing.T) {
	zones := makeTestZones(8, 524288)
	transport := newMockTransport(zones)

	smrDev, err := newSMRBackend(transport)
	require.NoError(t, err)

	result, err := smrDev.ReportZones(0, 4)
	require.NoError(t, err)
	// The mock returns all zones regardless of startLBA/count,
	// but we verify the backend calls through correctly.
	assert.NotEmpty(t, result)
}

func TestSMRBackendReadSectors(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)

	smrDev, err := newSMRBackend(transport)
	require.NoError(t, err)

	data, err := smrDev.ReadSectors(0, 8)
	require.NoError(t, err)
	assert.Len(t, data, 8*512)
	// Mock fills with 0xAA
	assert.Equal(t, byte(0xAA), data[0])
}

func TestSMRBackendWriteSectors(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)

	var capturedCDB []byte
	transport.scsiWriteFn = func(cdb []byte, _ []byte) error {
		capturedCDB = make([]byte, len(cdb))
		copy(capturedCDB, cdb)
		return nil
	}

	smrDev, err := newSMRBackend(transport)
	require.NoError(t, err)

	data := make([]byte, 4*512)
	err = smrDev.WriteSectors(100, data)
	require.NoError(t, err)

	// Verify CDB was a WRITE(16) at LBA 100, count 4
	require.NotNil(t, capturedCDB)
	assert.Equal(t, byte(0x8A), capturedCDB[0])
	assert.Equal(t, uint64(100), binary.BigEndian.Uint64(capturedCDB[2:10]))
	assert.Equal(t, uint32(4), binary.BigEndian.Uint32(capturedCDB[10:14]))
}

func TestSMRBackendWriteSectorsAlignment(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)

	smrDev, err := newSMRBackend(transport)
	require.NoError(t, err)

	// Non-aligned data should fail
	err = smrDev.WriteSectors(0, make([]byte, 513))
	assert.ErrorIs(t, err, backend.ErrAlignmentError)
}

func TestSMRBackendZoneActions(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)

	var lastCDB []byte
	transport.scsiNoDataFn = func(cdb []byte) error {
		lastCDB = make([]byte, len(cdb))
		copy(lastCDB, cdb)
		return nil
	}

	smrDev, err := newSMRBackend(transport)
	require.NoError(t, err)

	tests := []struct {
		name   string
		fn     func(uint64) error
		action uint8
	}{
		{"OpenZone", smrDev.OpenZone, zbc.ZoneActionOpen},
		{"CloseZone", smrDev.CloseZone, zbc.ZoneActionClose},
		{"FinishZone", smrDev.FinishZone, zbc.ZoneActionFinish},
		{"ResetZone", smrDev.ResetZone, zbc.ZoneActionReset},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn(524288)
			require.NoError(t, err)

			assert.Equal(t, byte(zbc.OpcodeZoneAction), lastCDB[0])
			assert.Equal(t, tt.action, lastCDB[1])
			assert.Equal(t, uint64(524288), binary.BigEndian.Uint64(lastCDB[2:10]))
		})
	}
}

func TestSMRBackendClose(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)

	smrDev, err := newSMRBackend(transport)
	require.NoError(t, err)

	err = smrDev.Close()
	require.NoError(t, err)
	assert.True(t, transport.closed)
}

func TestVerifyDeviceHostManaged(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)
	// PDT=0x14 → Host-Managed, should succeed
	_, err := newSMRBackend(transport)
	require.NoError(t, err)
}

func TestVerifyDeviceVPDHostManaged(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)
	transport.pdt = 0x00        // standard disk
	transport.zonedModel = 0x02 // VPD says Host-Managed
	_, err := newSMRBackend(transport)
	require.NoError(t, err)
}

func TestVerifyDeviceHostAwareWarns(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)
	transport.pdt = 0x00        // standard disk
	transport.zonedModel = 0x01 // Host-Aware
	// Should succeed (with warning logged)
	_, err := newSMRBackend(transport)
	require.NoError(t, err)
}

func TestVerifyDeviceNonZoned(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)
	transport.pdt = 0x00        // standard disk
	transport.zonedModel = 0x00 // non-zoned
	_, err := newSMRBackend(transport)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a zoned block device")
	assert.True(t, transport.closed)
}

func TestVerifyDeviceUnsupportedPDT(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)
	transport.pdt = 0x05 // CD/DVD
	_, err := newSMRBackend(transport)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported device type")
	assert.True(t, transport.closed)
}

func TestVerifyDeviceReservedZonedModel(t *testing.T) {
	zones := makeTestZones(2, 524288)
	transport := newMockTransport(zones)
	transport.pdt = 0x00        // standard disk
	transport.zonedModel = 0x03 // reserved
	_, err := newSMRBackend(transport)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported zoned model")
	assert.True(t, transport.closed)
}

func TestSMRBackendDiscoverNoZones(t *testing.T) {
	transport := newMockTransport(nil)
	// Override to handle INQUIRY normally but return empty REPORT ZONES
	transport.scsiReadFn = func(cdb []byte, buf []byte) error {
		if cdb[0] == zbc.OpcodeInquiry {
			return transport.handleInquiry(cdb, buf)
		}
		// REPORT ZONES: no zones
		binary.BigEndian.PutUint32(buf[0:4], 0)
		return nil
	}

	_, err := newSMRBackend(transport)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no zones reported")
	// Transport should be closed on discover failure
	assert.True(t, transport.closed)
}

func TestSMRBackendDiscoverTransportError(t *testing.T) {
	transport := newMockTransport(nil)
	transport.scsiReadFn = func(_ []byte, _ []byte) error {
		return errors.New("device not ready")
	}

	_, err := newSMRBackend(transport)
	assert.Error(t, err)
	assert.True(t, transport.closed)
}
