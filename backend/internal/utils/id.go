// Package utils contains small shared helpers used across Sentinel.
package utils

import "github.com/google/uuid"

// NewID returns a new random (v4) UUID as a string. It is used to generate
// identifiers for monitors, notifications, and other entities.
func NewID() string {
	return uuid.NewString()
}
