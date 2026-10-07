package nettools

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// isRefused reports whether a dial error means the host answered with a
// reset: the port is closed. Windows reports WSAECONNREFUSED (10061).
func isRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, syscall.ECONNREFUSED)
}
