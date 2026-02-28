package smr

// ScsiTransport abstracts the OS-specific mechanism for sending SCSI CDBs
// to a device. The CDB byte sequences are OS-independent (SPC/ZBC spec);
// only the transport layer (SG_IO on Linux, SPTI on Windows) differs.
type ScsiTransport interface {
	// ScsiRead sends a SCSI command that reads data from the device.
	ScsiRead(cdb []byte, buf []byte, timeoutMs uint32) error

	// ScsiWrite sends a SCSI command that writes data to the device.
	ScsiWrite(cdb []byte, buf []byte, timeoutMs uint32) error

	// ScsiNoData sends a SCSI command with no data transfer phase.
	ScsiNoData(cdb []byte, timeoutMs uint32) error

	// Close releases the underlying device handle.
	Close() error
}
