package services

import (
	"errors"
	"fmt"
	"testing"
)

// The too-large message reaches the user as written, and it is the one
// failure a retry cannot fix.
func TestClassifyJobErrorKeepsTooLarge(t *testing.T) {
	tooLarge := fmt.Errorf("aggregating report data: %w", ErrReportTooLarge)
	if got := classifyJobError(tooLarge); got != "This report is too large to build: narrow the scope or shorten the period" {
		t.Errorf("classifyJobError(too large) = %q", got)
	}
	if !permanentJobError(tooLarge) {
		t.Error("a too-large report must not be retried")
	}
	transient := errors.New("aggregating report data: connection reset by peer")
	if permanentJobError(transient) {
		t.Error("a dropped connection must be retried")
	}
	if got := classifyJobError(transient); got != "could not gather the data for this report" {
		t.Errorf("classifyJobError(transient) = %q", got)
	}
}
