package services

import (
	"testing"
	"time"
)

// A report rendered with no zone configured must say UTC, never the server
// process's zone — the container's default is not a choice anyone made.
func TestReportData_ReportLocationDefaultsToUTC(t *testing.T) {
	var nilData *ReportData
	if nilData.ReportLocation() != time.UTC {
		t.Error("nil ReportData should report UTC")
	}
	if (&ReportData{}).ReportLocation() != time.UTC {
		t.Error("unset Location should report UTC")
	}
}
