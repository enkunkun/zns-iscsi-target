//go:build linux

package smr

import "fmt"

// Open opens a SATA SMR device at the given path and returns an SMRBackend.
// On Linux, the device is accessed via the SG_IO ioctl interface.
func Open(devicePath string) (*SMRBackend, error) {
	transport, err := newLinuxTransport(devicePath)
	if err != nil {
		return nil, fmt.Errorf("opening SMR device %q: %w", devicePath, err)
	}
	return newSMRBackend(transport)
}
