//go:build windows

package smr

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func TestScsiPassThroughDirectSize(t *testing.T) {
	// SCSI_PASS_THROUGH_DIRECT on 64-bit Windows is 56 bytes.
	// This validates our Go struct matches the C layout.
	var sptd scsiPassThroughDirect
	size := unsafe.Sizeof(sptd)
	assert.Equal(t, uintptr(56), size,
		"scsiPassThroughDirect size must match Windows SCSI_PASS_THROUGH_DIRECT (56 bytes on amd64)")
}

func TestScsiPassThroughDirectWithSenseLayout(t *testing.T) {
	var sptdws scsiPassThroughDirectWithSense

	// SenseInfoOffset should point to the SenseBuf field
	expectedOffset := unsafe.Offsetof(sptdws.SenseBuf)
	assert.Equal(t, uintptr(56), expectedOffset,
		"SenseBuf must immediately follow the SPTD header")

	totalSize := unsafe.Sizeof(sptdws)
	assert.Equal(t, uintptr(56+32), totalSize,
		"total struct must be SPTD header (56) + sense buffer (32)")
}

func TestIoctlCode(t *testing.T) {
	// Verify the IOCTL code matches the Windows SDK definition.
	assert.Equal(t, uint32(0x4D014), uint32(ioctlScsiPassThroughDirect))
}
