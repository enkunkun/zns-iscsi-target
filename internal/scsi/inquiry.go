package scsi

import (
	"encoding/binary"
)

// INQUIRY opcodes and VPD page codes.
const (
	OpcodeInquiry byte = 0x12

	VPDPageSupportedPages     byte = 0x00
	VPDPageUnitSerialNumber   byte = 0x80
	VPDPageDeviceIdentifiers  byte = 0x83
)

// targetIQN is used as the basis for device identification.
// It is populated by the Handler when constructed.
var defaultSerialNumber = "ZNS0000000001"

// handleInquiry processes an INQUIRY CDB and returns the response data.
// cdb must be at least 6 bytes.
// devID is a 16-byte binary identifier used for VPD page 0x83.
func handleInquiry(cdb []byte, serialNumber string, devID [16]byte) ([]byte, error) {
	if len(cdb) < 6 {
		return nil, nil
	}

	evpd := cdb[1]&0x01 != 0 // bit 0 of byte 1: Enable Vital Product Data
	pageCode := cdb[2]

	if evpd {
		return buildVPDPage(pageCode, serialNumber, devID)
	}
	return buildStandardInquiry(), nil
}

// buildStandardInquiry builds a standard INQUIRY response (36 bytes minimum).
func buildStandardInquiry() []byte {
	buf := make([]byte, 36)
	buf[0] = 0x00 // Peripheral device type: Direct-access block device
	buf[1] = 0x00 // RMB=0: not removable
	buf[2] = 0x05 // Version: SPC-3
	buf[3] = 0x12 // Response data format=2 (SPC), HiSup=1 (hierarchical addressing)
	buf[4] = 0x1F // Additional length: 31 bytes (total - 5 = 36 - 5)
	buf[5] = 0x00 // SCCS=0, ACC=0, TPGS=0, 3PC=0, PROTECT=0
	buf[6] = 0x00 // ENCSERV=0, VS=0, MULTIP=0, MCHNGR=0
	buf[7] = 0x00 // Vendor-specific

	// Vendor identification (bytes 8-15): 8 bytes, left-justified, space-padded
	copy(buf[8:16], []byte("ZNS     "))

	// Product identification (bytes 16-31): 16 bytes, left-justified, space-padded
	copy(buf[16:32], []byte("iSCSI ZNS Target"))

	// Product revision level (bytes 32-35): 4 bytes
	copy(buf[32:36], []byte("1.00"))

	return buf
}

// buildVPDPage builds a VPD page response.
func buildVPDPage(pageCode byte, serialNumber string, devID [16]byte) ([]byte, error) {
	switch pageCode {
	case VPDPageSupportedPages:
		return buildVPDPage00(), nil
	case VPDPageUnitSerialNumber:
		return buildVPDPage80(serialNumber), nil
	case VPDPageDeviceIdentifiers:
		return buildVPDPage83(devID), nil
	default:
		return nil, nil // caller converts nil to ILLEGAL REQUEST
	}
}

// buildVPDPage00 builds VPD page 0x00: Supported VPD Pages.
func buildVPDPage00() []byte {
	supported := []byte{VPDPageSupportedPages, VPDPageUnitSerialNumber, VPDPageDeviceIdentifiers}
	buf := make([]byte, 4+len(supported))
	buf[0] = 0x00                       // Peripheral qualifier + device type
	buf[1] = VPDPageSupportedPages      // Page code
	buf[2] = 0x00                       // Reserved
	buf[3] = byte(len(supported))       // Page length
	copy(buf[4:], supported)
	return buf
}

// buildVPDPage80 builds VPD page 0x80: Unit Serial Number.
func buildVPDPage80(serialNumber string) []byte {
	sn := []byte(serialNumber)
	buf := make([]byte, 4+len(sn))
	buf[0] = 0x00                   // Peripheral qualifier + device type
	buf[1] = VPDPageUnitSerialNumber // Page code
	buf[2] = 0x00                   // Reserved
	buf[3] = byte(len(sn))          // Page length
	copy(buf[4:], sn)
	return buf
}

// buildVPDPage83 builds VPD page 0x83: Device Identification.
// Uses NAA type 6 (EUI-64 based), 16 bytes of binary identifier.
func buildVPDPage83(devID [16]byte) []byte {
	// Designation descriptor: NAA type 6, 16 bytes
	// Code set 1 (binary), Association 0 (addressed LU), Designator type 3 (NAA)
	descLen := 16
	descriptor := make([]byte, 4+descLen)
	descriptor[0] = 0x01                   // Code set: binary
	descriptor[1] = 0x03                   // Designator type: NAA
	descriptor[2] = 0x00                   // Reserved
	descriptor[3] = byte(descLen)          // Designator length
	// NAA type 6 identifier: first nibble of byte 4 = 6
	descriptor[4] = (0x06 << 4) | (devID[0] >> 4)
	for i := 1; i < 16; i++ {
		descriptor[3+i] = (devID[i-1]<<4 | devID[i]>>4)
	}
	// Alternatively, just copy devID directly with NAA nibble set
	copy(descriptor[4:], devID[:])
	descriptor[4] = (descriptor[4] & 0x0F) | 0x60 // NAA=6, high nibble

	pageLen := len(descriptor)
	buf := make([]byte, 4+pageLen)
	buf[0] = 0x00                       // Peripheral qualifier + device type
	buf[1] = VPDPageDeviceIdentifiers   // Page code
	binary.BigEndian.PutUint16(buf[2:4], uint16(pageLen))
	copy(buf[4:], descriptor)
	return buf
}
