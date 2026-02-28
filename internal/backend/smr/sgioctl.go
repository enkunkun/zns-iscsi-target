//go:build linux

// Package smr provides a SATA SMR (Shingled Magnetic Recording) backend
// that communicates with real devices via SG_IO ioctl.
package smr

import (
	"fmt"
	"syscall"
	"unsafe"
)

// SG_IO ioctl number on Linux.
const sgIOIoctl = 0x2285

// SG_DXFER direction constants.
const (
	sgDxferNone      = -1
	sgDxferToDev     = -2
	sgDxferFromDev   = -3
	sgDxferToFromDev = -4
)

// sgIOHdr is the Go representation of sg_io_hdr_t.
// Fields are laid out exactly as the kernel struct.
type sgIOHdr struct {
	interfaceID  int32
	dxferDirection int32
	cmdLen       uint8
	mxSbLen      uint8
	iovecCount   uint16
	dxferLen     uint32
	dxferp       uintptr
	cmdp         uintptr
	sbp          uintptr
	timeout      uint32
	flags        uint32
	packID       int32
	_            uintptr
	status       uint8
	maskedStatus uint8
	msgStatus    uint8
	sbLenWr      uint8
	hostStatus   uint16
	driverStatus uint16
	resid        int32
	duration     uint32
	info         uint32
}

// sgio executes an SG_IO ioctl on the given file descriptor.
func sgio(fd int, hdr *sgIOHdr) error {
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(sgIOIoctl),
		uintptr(unsafe.Pointer(hdr)),
	)
	if errno != 0 {
		return fmt.Errorf("SG_IO ioctl: %w", errno)
	}
	if hdr.status != 0 {
		return fmt.Errorf("SCSI status 0x%02x, host_status 0x%02x, driver_status 0x%02x",
			hdr.status, hdr.hostStatus, hdr.driverStatus)
	}
	return nil
}

// sgRead performs a SCSI READ via SG_IO.
func sgRead(fd int, cdb []byte, buf []byte, timeoutMs uint32) error {
	if len(buf) == 0 {
		return nil
	}
	sense := make([]byte, 32)
	hdr := sgIOHdr{
		interfaceID:  'S',
		dxferDirection: sgDxferFromDev,
		cmdLen:       uint8(len(cdb)),
		mxSbLen:      uint8(len(sense)),
		dxferLen:     uint32(len(buf)),
		dxferp:       uintptr(unsafe.Pointer(&buf[0])),
		cmdp:         uintptr(unsafe.Pointer(&cdb[0])),
		sbp:          uintptr(unsafe.Pointer(&sense[0])),
		timeout:      timeoutMs,
	}
	return sgio(fd, &hdr)
}

// sgWrite performs a SCSI WRITE via SG_IO.
func sgWrite(fd int, cdb []byte, buf []byte, timeoutMs uint32) error {
	if len(buf) == 0 {
		return nil
	}
	sense := make([]byte, 32)
	hdr := sgIOHdr{
		interfaceID:  'S',
		dxferDirection: sgDxferToDev,
		cmdLen:       uint8(len(cdb)),
		mxSbLen:      uint8(len(sense)),
		dxferLen:     uint32(len(buf)),
		dxferp:       uintptr(unsafe.Pointer(&buf[0])),
		cmdp:         uintptr(unsafe.Pointer(&cdb[0])),
		sbp:          uintptr(unsafe.Pointer(&sense[0])),
		timeout:      timeoutMs,
	}
	return sgio(fd, &hdr)
}

// sgNoData performs a SCSI command with no data transfer.
func sgNoData(fd int, cdb []byte, timeoutMs uint32) error {
	sense := make([]byte, 32)
	hdr := sgIOHdr{
		interfaceID:  'S',
		dxferDirection: sgDxferNone,
		cmdLen:       uint8(len(cdb)),
		mxSbLen:      uint8(len(sense)),
		dxferLen:     0,
		cmdp:         uintptr(unsafe.Pointer(&cdb[0])),
		sbp:          uintptr(unsafe.Pointer(&sense[0])),
		timeout:      timeoutMs,
	}
	return sgio(fd, &hdr)
}
