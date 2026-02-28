package scsi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleInquiryStandard(t *testing.T) {
	cdb := make([]byte, 6)
	cdb[0] = OpcodeInquiry
	// EVPD=0 (standard INQUIRY)

	data, err := handleInquiry(cdb, "TEST001", [16]byte{})
	require.NoError(t, err)
	require.NotNil(t, data)
	require.GreaterOrEqual(t, len(data), 36)

	t.Run("peripheral device type is 0x00 (disk)", func(t *testing.T) {
		assert.Equal(t, byte(0x00), data[0])
	})

	t.Run("version is 0x05 (SPC-3)", func(t *testing.T) {
		assert.Equal(t, byte(0x05), data[2])
	})

	t.Run("response data format is 2", func(t *testing.T) {
		assert.Equal(t, byte(0x12), data[3]) // HiSup bit set + format 2
	})

	t.Run("additional length correct", func(t *testing.T) {
		// additional length = total - 5
		assert.Equal(t, byte(len(data)-5), data[4])
	})

	t.Run("vendor identification present", func(t *testing.T) {
		vendor := string(data[8:16])
		assert.Contains(t, vendor, "ZNS")
	})

	t.Run("product identification present", func(t *testing.T) {
		product := string(data[16:32])
		assert.Contains(t, product, "ZNS")
	})
}

func TestHandleInquiryVPD00(t *testing.T) {
	cdb := make([]byte, 6)
	cdb[0] = OpcodeInquiry
	cdb[1] = 0x01 // EVPD=1
	cdb[2] = VPDPageSupportedPages

	data, err := handleInquiry(cdb, "TEST001", [16]byte{})
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, VPDPageSupportedPages, data[1], "page code must be 0x00")

	pageLen := int(data[3])
	pages := data[4 : 4+pageLen]

	// Must include pages 0x00, 0x80, 0x83
	assert.Contains(t, pages, VPDPageSupportedPages)
	assert.Contains(t, pages, VPDPageUnitSerialNumber)
	assert.Contains(t, pages, VPDPageDeviceIdentifiers)
}

func TestHandleInquiryVPD80(t *testing.T) {
	cdb := make([]byte, 6)
	cdb[0] = OpcodeInquiry
	cdb[1] = 0x01 // EVPD=1
	cdb[2] = VPDPageUnitSerialNumber

	serial := "ZNSTESTSERIAL01"
	data, err := handleInquiry(cdb, serial, [16]byte{})
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, VPDPageUnitSerialNumber, data[1])
	pageLen := int(data[3])
	sn := string(data[4 : 4+pageLen])
	assert.Equal(t, serial, sn)
}

func TestHandleInquiryVPD83(t *testing.T) {
	cdb := make([]byte, 6)
	cdb[0] = OpcodeInquiry
	cdb[1] = 0x01 // EVPD=1
	cdb[2] = VPDPageDeviceIdentifiers

	var devID [16]byte
	devID[0] = 0x12
	devID[1] = 0x34

	data, err := handleInquiry(cdb, "TEST001", devID)
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, VPDPageDeviceIdentifiers, data[1])

	// Check NAA type 6 in descriptor
	descriptor := data[4:]
	require.GreaterOrEqual(t, len(descriptor), 5)
	naaType := (descriptor[4] >> 4) & 0x0F
	assert.Equal(t, byte(6), naaType, "NAA type must be 6")
}

func TestHandleInquiryShortCDB(t *testing.T) {
	cdb := []byte{OpcodeInquiry} // too short
	data, err := handleInquiry(cdb, "TEST", [16]byte{})
	assert.NoError(t, err)
	assert.Nil(t, data)
}
