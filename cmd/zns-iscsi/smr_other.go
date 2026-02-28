//go:build !linux && !windows

package main

import (
	"fmt"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
)

// openSMRBackend is not supported on non-Linux platforms.
func openSMRBackend(_ string) (backend.ZonedDevice, error) {
	return nil, fmt.Errorf("SMR backend is only supported on Linux and Windows")
}
