//go:build windows

package smr

// IOCTL_SCSI_PASS_THROUGH_DIRECT is the Windows device I/O control code
// for sending SCSI commands via the SPTI (SCSI Pass-Through Interface).
//
// CTL_CODE(IOCTL_SCSI_BASE, 0x0405, METHOD_BUFFERED, FILE_READ_ACCESS | FILE_WRITE_ACCESS)
// = (0x04 << 16) | (0x03 << 14) | (0x0405 << 2) | 0 = 0x4D014
const ioctlScsiPassThroughDirect = 0x4D014

// SCSI data direction constants for SPTI.
const (
	scsiIoctlDataOut    = 0 // host-to-device (write)
	scsiIoctlDataIn     = 1 // device-to-host (read)
	scsiIoctlDataUnspec = 2 // no data transfer
)

// scsiPassThroughDirect mirrors the Windows SCSI_PASS_THROUGH_DIRECT structure.
// Layout must match the C struct exactly for DeviceIoControl interop.
//
//	typedef struct _SCSI_PASS_THROUGH_DIRECT {
//	    USHORT Length;              // sizeof(SCSI_PASS_THROUGH_DIRECT)
//	    UCHAR  ScsiStatus;
//	    UCHAR  PathId;
//	    UCHAR  TargetId;
//	    UCHAR  Lun;
//	    UCHAR  CdbLength;
//	    UCHAR  SenseInfoLength;
//	    UCHAR  DataIn;
//	    UCHAR  _padding;           // alignment
//	    ULONG  DataTransferLength;
//	    ULONG  TimeOutValue;       // in seconds
//	    PVOID  DataBuffer;
//	    ULONG  SenseInfoOffset;
//	    UCHAR  Cdb[16];
//	}
type scsiPassThroughDirect struct {
	Length             uint16
	ScsiStatus         uint8
	PathId             uint8
	TargetId           uint8
	Lun                uint8
	CdbLength          uint8
	SenseInfoLength    uint8
	DataIn             uint8
	_                  uint8  // padding for alignment
	DataTransferLength uint32
	TimeOutValue       uint32 // seconds
	DataBuffer         uintptr
	SenseInfoOffset    uint32
	Cdb                [16]byte
}

// scsiPassThroughDirectWithSense embeds the pass-through header followed by
// an inline sense buffer. SenseInfoOffset points past the header into SenseBuf.
type scsiPassThroughDirectWithSense struct {
	Sptd     scsiPassThroughDirect
	SenseBuf [32]byte
}
