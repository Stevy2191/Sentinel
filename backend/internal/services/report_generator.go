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
}

// NewReportGenerator returns a generator bound to its dependencies.
func NewReportGenerator(db *gorm.DB, aggregator *ReportAggregatorService, pdfRenderer *PDFRendererService) *ReportGenerator {
	return &ReportGenerator{db: db, aggregator: aggregator, pdfRenderer: pdfRenderer}
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
	var template models.ReportTemplate
	if err := rg.db.WithContext(ctx).First(&template, "id = ?", report.TemplateID).Error; err != nil {
		return nil, fmt.Errorf("loading report template: %w", err)
	}

	data, err := rg.aggregator.AggregateReportData(ctx, report, generatedBy)
	if err != nil {
		return nil, fmt.Errorf("aggregating report data: %w", err)
	}

	filename, err := rg.pdfRenderer.RenderReportToPDF(data, template.Sections, "report_"+report.ID.String()[:8])
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
