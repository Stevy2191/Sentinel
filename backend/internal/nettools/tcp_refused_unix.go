//go:build !windows

package nettools

import (
	"errors"
	"syscall"
)

// isRefused reports whether a dial error means the host answered with a
// reset: the port is closed.
func isRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
