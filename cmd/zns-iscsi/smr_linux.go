//go:build linux

package main

import (
	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/internal/backend/smr"
)

// openSMRBackend opens an SMR block device at the given path.
// Only available on Linux.
func openSMRBackend(path string) (backend.ZonedDevice, error) {
	return smr.Open(path)
}
