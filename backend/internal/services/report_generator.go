// Package services - report_generator.go is the single path that turns a saved
// report definition into a rendered artifact and a generation record. Both the
// HTTP handler and the scheduler go through it, so an on-demand report and a
// scheduled one cannot drift apart.
package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ReportGenerator renders a report and records the generation.
type ReportGenerator struct {
	db          *gorm.DB
	aggregator  *ReportAggregatorService
	pdfRenderer *PDFRendererService
	// settings resolves the report timezone at render time, so changing it in
	// Settings affects the next report rather than requiring a restart. Nil is
	// tolerated and means UTC, which keeps the zero value usable in tests.
	settings *SettingsService
}

// NewReportGenerator returns a generator bound to its dependencies.
func NewReportGenerator(db *gorm.DB, aggregator *ReportAggregatorService, pdfRenderer *PDFRendererService, settings *SettingsService) *ReportGenerator {
	return &ReportGenerator{db: db, aggregator: aggregator, pdfRenderer: pdfRenderer, settings: settings}
}

// reportLocation is the configured zone, or UTC when no settings service is
// bound. Never the process zone: see models.DefaultReportTimezone.
func (rg *ReportGenerator) reportLocation(ctx context.Context) *time.Location {
	if rg.settings == nil {
		return time.UTC
	}
	return rg.settings.ReportLocation(ctx)
}

// GeneratedReport is the outcome of one generation.
type GeneratedReport struct {
	Generation *models.ReportGeneration
	Data       *ReportData
	// Path is the absolute location of the rendered PDF, for attaching to email.
	Path string
}

// GenerateAndSaveReport aggregates, renders, and records a report.
func (rg *ReportGenerator) GenerateAndSaveReport(ctx context.Context, report *models.Report, generatedBy uuid.UUID) (*GeneratedReport, error) {
	data, err := rg.aggregator.AggregateReportData(ctx, report, generatedBy)
	if err != nil {
		return nil, fmt.Errorf("aggregating report data: %w", err)
	}

	// Stamped before rendering so both the PDF and anything else built from
	// this data describe the same clock.
	data.Location = rg.reportLocation(ctx)

	filename, err := rg.pdfRenderer.RenderReportToPDF(data, report.ReportType, "report_"+report.ID.String()[:8])
	if err != nil {
		return nil, fmt.Errorf("rendering report PDF: %w", err)
	}

	path, err := rg.pdfRenderer.GetPDFPath(filename)
	if err != nil {
		return nil, err
	}

	// A failed stat leaves file_size null rather than recording a wrong zero.
	var sizePtr *int
	if size, sizeErr := rg.pdfRenderer.GetPDFFileSize(filename); sizeErr == nil {
		sizePtr = &size
	}

	generation := &models.ReportGeneration{
		ID:          uuid.New(),
		ReportID:    report.ID,
		GeneratedAt: time.Now(),
		PDFPath:     filename,
		FileSize:    sizePtr,
		GeneratedBy: generatedBy,
	}
	if err := rg.db.WithContext(ctx).Create(generation).Error; err != nil {
		return nil, fmt.Errorf("saving report generation: %w", err)
	}

	return &GeneratedReport{Generation: generation, Data: data, Path: path}, nil
}
