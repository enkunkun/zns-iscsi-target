//go:build windows

package smr

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsTransport implements ScsiTransport using the Windows SPTI interface.
type windowsTransport struct {
	handle windows.Handle
}

// newWindowsTransport opens a physical drive (e.g. `\\.\PhysicalDrive1`) and
// returns a windowsTransport. Administrator privileges are required.
func newWindowsTransport(devicePath string) (*windowsTransport, error) {
	pathPtr, err := windows.UTF16PtrFromString(devicePath)
	if err != nil {
		return nil, fmt.Errorf("invalid device path %q: %w", devicePath, err)
	}

	h, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("opening device %q (run as Administrator): %w", devicePath, err)
	}

	return &windowsTransport{handle: h}, nil
}

func (t *windowsTransport) ScsiRead(cdb []byte, buf []byte, timeoutMs uint32) error {
	return t.execute(cdb, buf, scsiIoctlDataIn, timeoutMs)
}

func (t *windowsTransport) ScsiWrite(cdb []byte, buf []byte, timeoutMs uint32) error {
	return t.execute(cdb, buf, scsiIoctlDataOut, timeoutMs)
}

func (t *windowsTransport) ScsiNoData(cdb []byte, timeoutMs uint32) error {
	return t.execute(cdb, nil, scsiIoctlDataUnspec, timeoutMs)
}

func (t *windowsTransport) Close() error {
	return windows.CloseHandle(t.handle)
}

// execute sends a SCSI CDB via IOCTL_SCSI_PASS_THROUGH_DIRECT.
func (t *windowsTransport) execute(cdb []byte, buf []byte, direction uint8, timeoutMs uint32) error {
	var sptdws scsiPassThroughDirectWithSense

	sptd := &sptdws.Sptd
	sptd.Length = uint16(unsafe.Sizeof(*sptd))
	sptd.CdbLength = uint8(len(cdb))
	sptd.SenseInfoLength = uint8(len(sptdws.SenseBuf))
	sptd.DataIn = direction
	sptd.DataTransferLength = uint32(len(buf))
	sptd.TimeOutValue = (timeoutMs + 999) / 1000 // convert ms to seconds, round up
	sptd.SenseInfoOffset = uint32(unsafe.Offsetof(sptdws.SenseBuf))

	if len(buf) > 0 {
		sptd.DataBuffer = uintptr(unsafe.Pointer(&buf[0]))
	}

	copy(sptd.Cdb[:], cdb)

	var bytesReturned uint32
	err := windows.DeviceIoControl(
		t.handle,
		ioctlScsiPassThroughDirect,
		(*byte)(unsafe.Pointer(&sptdws)),
		uint32(unsafe.Sizeof(sptdws)),
		(*byte)(unsafe.Pointer(&sptdws)),
		uint32(unsafe.Sizeof(sptdws)),
		&bytesReturned,
		nil,
	)
	if err != nil {
		return fmt.Errorf("IOCTL_SCSI_PASS_THROUGH_DIRECT (run as Administrator): %w", err)
	}

	if sptd.ScsiStatus != 0 {
		return fmt.Errorf("SCSI status 0x%02x (sense: % x)",
			sptd.ScsiStatus, sptdws.SenseBuf[:sptd.SenseInfoLength])
	}

	return nil
}
