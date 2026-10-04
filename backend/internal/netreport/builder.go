// Package netreport builds the Metrics report. It resolves a network scope
// (ports, port roles at sites, devices, sites) as the report's owner through
// site access, reads the statistics the PDF shows from the 5-minute rollup,
// and fills a services.MetricsReportData. It lives outside services, as
// dashboards does, so services never imports it: the report pipeline reaches
// it through services.NetworkReportBuilder.
package netreport

import (
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// User-facing messages, returned verbatim in the API's 400 responses.
const (
	// MsgNotAvailable answers a chosen port, device or site that is hidden
	// from the requester or no longer exists: one message for both, so the
	// editor cannot be used to probe ids.
	MsgNotAvailable = "A chosen port, device or site is not available"
	// MsgTextMetric refuses a metric whose values are codes, not amounts.
	MsgTextMetric = "text metrics cannot be reported"
)

// FieldError is a create/preview validation error the API returns as 400 {error: Message}.
type FieldError struct{ Field, Message string }

// Error is the message alone: it is what the user reads.
func (e *FieldError) Error() string { return e.Message }

// Builder resolves network scopes and builds Metrics reports.
type Builder struct {
	db      *gorm.DB
	metrics *services.MetricsStore
	sites   *services.SiteService
}

// NewBuilder returns a Builder reading db and the shared metrics store.
func NewBuilder(db *gorm.DB, metrics *services.MetricsStore) *Builder {
	return &Builder{db: db, metrics: metrics, sites: services.NewSiteService(db)}
}
