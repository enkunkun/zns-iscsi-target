//go:build windows

package main

import (
	"os"
	"os/signal"
)

// notifySignals registers OS-appropriate shutdown signals.
// On Windows: os.Interrupt (Ctrl+C).
func notifySignals(ch chan<- os.Signal) {
	signal.Notify(ch, os.Interrupt)
}
