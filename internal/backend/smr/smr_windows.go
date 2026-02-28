//go:build windows

package smr

import "fmt"

// Open opens a SATA SMR device at the given path and returns an SMRBackend.
// On Windows, the device is accessed via the SPTI (SCSI Pass-Through Interface).
// The path should be in the form `\\.\PhysicalDriveN` (e.g. `\\.\PhysicalDrive1`).
// Administrator privileges are required.
func Open(devicePath string) (*SMRBackend, error) {
	transport, err := newWindowsTransport(devicePath)
	if err != nil {
		return nil, fmt.Errorf("opening SMR device %q: %w", devicePath, err)
	}
	return newSMRBackend(transport)
}
