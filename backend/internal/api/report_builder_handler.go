package api

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// This file is the entry point for the saved-report builder: user-defined report
// definitions, PDF generation, generation history, and sharing. It is separate
// from report_handler.go, which serves the existing live/ad-hoc report endpoints
// (monitor report, uptime history, timeline, summary) and is already large.
//
// Only the wiring exists so far. The endpoints land in the next phase:
//
//	POST   /api/v1/reports/generate      render a report definition to a PDF
//	GET    /api/v1/reports               list report definitions visible to the caller
//	GET    /api/v1/reports/:id/download  download the latest generation
//	POST   /api/v1/reports/:id/share     mint a share token
//	GET    /api/v1/reports/share/:token  public, token-authenticated fetch
//	PATCH  /api/v1/monitors/:id          set sla_target
//
// Two notes for whoever implements them. The share-token route must be mounted
// on the router rather than the v1 group, which is behind AuthMiddleware - the
// same split RegisterPublicStatusRoutes already uses. And PATCH /monitors/:id
// belongs with the monitor routes, not here, so it stays consistent with the
// rest of monitor editing.

// ReportBuilder holds the dependencies the report-builder endpoints need.
type ReportBuilder struct {
	db         *gorm.DB
	aggregator *services.ReportAggregatorService
}

// NewReportBuilder returns a handler set bound to db and the aggregator.
func NewReportBuilder(db *gorm.DB, aggregator *services.ReportAggregatorService) *ReportBuilder {
	return &ReportBuilder{db: db, aggregator: aggregator}
}

// RegisterReportBuilderRoutes mounts the report-builder endpoints on rg.
//
// It registers nothing yet: the handlers above are still to be written, and
// mounting routes that return nothing would advertise an API that does not work.
// The function exists so main.go's wiring is in place and the service is
// constructed and reachable.
func RegisterReportBuilderRoutes(rg *gin.RouterGroup, builder *ReportBuilder) {
	_ = rg
	_ = builder
}
