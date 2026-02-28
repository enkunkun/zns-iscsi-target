//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifySignals registers OS-appropriate shutdown signals.
// On Unix systems: SIGTERM and SIGINT.
func notifySignals(ch chan<- os.Signal) {
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
}
