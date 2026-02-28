//go:build linux

package smr

import "os"

// linuxTransport implements ScsiTransport using Linux SG_IO ioctl.
type linuxTransport struct {
	fd   int
	file *os.File
}

// newLinuxTransport opens the given device path and returns a linuxTransport.
func newLinuxTransport(devicePath string) (*linuxTransport, error) {
	f, err := os.OpenFile(devicePath, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &linuxTransport{
		fd:   int(f.Fd()),
		file: f,
	}, nil
}

func (t *linuxTransport) ScsiRead(cdb []byte, buf []byte, timeoutMs uint32) error {
	return sgRead(t.fd, cdb, buf, timeoutMs)
}

func (t *linuxTransport) ScsiWrite(cdb []byte, buf []byte, timeoutMs uint32) error {
	return sgWrite(t.fd, cdb, buf, timeoutMs)
}

func (t *linuxTransport) ScsiNoData(cdb []byte, timeoutMs uint32) error {
	return sgNoData(t.fd, cdb, timeoutMs)
}

func (t *linuxTransport) Close() error {
	return t.file.Close()
}
