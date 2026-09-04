package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// This file serves the saved-report builder: report definitions, PDF generation,
// generation history, and sharing. It is separate from report_handler.go, which
// serves the existing live/ad-hoc report endpoints and is already large.
//
// Responses use respondSuccess/respondError so the payload envelope matches the
// rest of the API and the frontend's axios client, which unwraps {success,data}.

// ReportBuilder holds the dependencies the report-builder endpoints need.
type ReportBuilder struct {
	db            *gorm.DB
	aggregator    *services.ReportAggregatorService
	pdfRenderer   *services.PDFRendererService
	htmlGenerator *services.HTMLReportGenerator
}

// NewReportBuilder returns a handler set bound to its dependencies.
func NewReportBuilder(
	db *gorm.DB,
	aggregator *services.ReportAggregatorService,
	pdfRenderer *services.PDFRendererService,
) *ReportBuilder {
	return &ReportBuilder{
		db:            db,
		aggregator:    aggregator,
		pdfRenderer:   pdfRenderer,
		htmlGenerator: services.NewHTMLReportGenerator(),
	}
}

// ---- DTOs ------------------------------------------------------------------

// GenerateReportRequest creates a report definition and renders it immediately.
type GenerateReportRequest struct {
	Name              string             `json:"name" binding:"required"`
	TemplateID        uuid.UUID          `json:"template_id" binding:"required"`
	ScopeType         string             `json:"scope_type" binding:"required,oneof=monitors tags groups"`
	ScopeData         models.ReportScope `json:"scope_data" binding:"required"`
	TimeRangeDays     int                `json:"time_range_days" binding:"required,min=1,max=365"`
	CustomTitle       *string            `json:"custom_title"`
	CustomDescription *string            `json:"custom_description"`
}

// ReportResponse is a report definition plus its generation history.
type ReportResponse struct {
	ID            uuid.UUID                  `json:"id"`
	Name          string                     `json:"name"`
	TemplateName  string                     `json:"template_name"`
	ScopeType     string                     `json:"scope_type"`
	TimeRangeDays int                        `json:"time_range_days"`
	CreatedAt     time.Time                  `json:"created_at"`
	UpdatedAt     time.Time                  `json:"updated_at"`
	LastGenerated *time.Time                 `json:"last_generated"`
	Generations   []ReportGenerationResponse `json:"generations"`
}

// ReportGenerationResponse is one rendered artifact.
type ReportGenerationResponse struct {
	ID          uuid.UUID `json:"id"`
	GeneratedAt time.Time `json:"generated_at"`
	FileSize    *int      `json:"file_size"`
	DownloadURL string    `json:"download_url"`
}

// ShareReportResponse carries a freshly minted share token.
type ShareReportResponse struct {
	ShareToken string     `json:"share_token"`
	ShareLink  string     `json:"share_link"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

// ---- handlers --------------------------------------------------------------

// GenerateReport handles POST /api/v1/reports/generate. It saves a new report
// definition, aggregates its data, and renders a PDF in one call.
func (h *ReportBuilder) GenerateReport(c *gin.Context) {
	var req GenerateReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	userID, _, _, ok := GetUserFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var template models.ReportTemplate
	if err := h.db.WithContext(c.Request.Context()).
		First(&template, "id = ?", req.TemplateID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "report template not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "loading report template: "+err.Error())
		return
	}

	report := models.Report{
		ID:                uuid.New(),
		UserID:            userID,
		Name:              req.Name,
		TemplateID:        req.TemplateID,
		ScopeType:         req.ScopeType,
		ScopeData:         req.ScopeData,
		TimeRangeDays:     req.TimeRangeDays,
		CustomTitle:       req.CustomTitle,
		CustomDescription: req.CustomDescription,
		CreatedBy:         userID,
	}
	// Validate before writing: an empty scope would otherwise persist a report
	// that can only ever render as blank.
	if err := report.Validate(); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	// Aggregate and render BEFORE persisting, so a failure does not leave an
	// orphaned report definition behind.
	reportData, err := h.aggregator.AggregateReportData(c.Request.Context(), &report)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "aggregating report data: "+err.Error())
		return
	}

	pdfFilename, err := h.pdfRenderer.RenderReportToPDF(
		reportData, template.Sections, "report_"+report.ID.String()[:8])
	if err != nil {
		respondError(c, http.StatusInternalServerError, "rendering report PDF: "+err.Error())
		return
	}

	fileSize, sizeErr := h.pdfRenderer.GetPDFFileSize(pdfFilename)
	var fileSizePtr *int
	if sizeErr == nil {
		fileSizePtr = &fileSize
	}

	generation := models.ReportGeneration{
		ID:          uuid.New(),
		ReportID:    report.ID,
		GeneratedAt: time.Now(),
		PDFPath:     pdfFilename,
		FileSize:    fileSizePtr,
		GeneratedBy: userID,
	}
	access := models.ReportAccess{
		ID:         uuid.New(),
		ReportID:   report.ID,
		UserID:     &userID,
		AccessType: models.AccessTypeOwner,
	}

	// One transaction: a report with no owner row would be invisible to its own
	// creator in ListReports.
	if err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&report).Error; err != nil {
			return err
		}
		if err := tx.Create(&access).Error; err != nil {
			return err
		}
		return tx.Create(&generation).Error
	}); err != nil {
		respondError(c, http.StatusInternalServerError, "saving report: "+err.Error())
		return
	}

	respondSuccess(c, http.StatusCreated, gin.H{
		"id":            report.ID,
		"generation_id": generation.ID,
		"download_url":  downloadURL(report.ID, generation.ID),
		"warnings":      reportData.Warnings,
	})
}

// ListReports handles GET /api/v1/reports. Admins see every report; everyone
// else sees the ones they created or have been granted access to.
func (h *ReportBuilder) ListReports(c *gin.Context) {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	ctx := c.Request.Context()
	query := h.db.WithContext(ctx).Model(&models.Report{})
	if !isAdmin {
		query = query.Where(
			"created_by = ? OR id IN (SELECT report_id FROM report_access WHERE user_id = ?)",
			userID, userID)
	}

	var reports []models.Report
	if err := query.Order("created_at DESC").Find(&reports).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "listing reports: "+err.Error())
		return
	}

	// Always a slice, never null - the frontend maps over this directly.
	out := make([]ReportResponse, 0, len(reports))
	for i := range reports {
		resp, err := h.buildReportResponse(ctx, &reports[i], "")
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, resp)
	}
	respondSuccess(c, http.StatusOK, out)
}

// DownloadReport handles GET /api/v1/reports/:id/download/:generation_id.
func (h *ReportBuilder) DownloadReport(c *gin.Context) {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	reportID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid report id")
		return
	}
	generationID, err := uuid.Parse(c.Param("generation_id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid generation id")
		return
	}

	ctx := c.Request.Context()
	var report models.Report
	if err := h.db.WithContext(ctx).First(&report, "id = ?", reportID).Error; err != nil {
		respondError(c, http.StatusNotFound, "report not found")
		return
	}

	if !h.userCanRead(ctx, &report, userID, isAdmin) {
		respondError(c, http.StatusForbidden, "you do not have permission to access this report")
		return
	}

	h.serveGeneration(c, reportID, generationID)
}

// ShareReport handles POST /api/v1/reports/:id/share, minting a share token.
// Only the report's owner or an admin may share it.
func (h *ReportBuilder) ShareReport(c *gin.Context) {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	reportID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid report id")
		return
	}

	ctx := c.Request.Context()
	var report models.Report
	if err := h.db.WithContext(ctx).First(&report, "id = ?", reportID).Error; err != nil {
		respondError(c, http.StatusNotFound, "report not found")
		return
	}
	// Sharing is a stronger right than reading: being a viewer is not enough.
	if report.CreatedBy != userID && !isAdmin {
		respondError(c, http.StatusForbidden, "only the report owner can share it")
		return
	}

	token, err := generateShareToken()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "generating share token: "+err.Error())
		return
	}

	access := models.ReportAccess{
		ID:         uuid.New(),
		ReportID:   report.ID,
		AccessType: models.AccessTypeViewer,
		ShareToken: &token,
	}
	if err := h.db.WithContext(ctx).Create(&access).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "creating share link: "+err.Error())
		return
	}

	respondSuccess(c, http.StatusOK, ShareReportResponse{
		ShareToken: token,
		ShareLink:  "/reports/share/" + token,
		// Expiry is not implemented; the field is present so a caller can see
		// plainly that the link does not expire.
		ExpiresAt: nil,
	})
}

// ViewSharedReport handles GET /api/v1/public/reports/share/:token. Public.
func (h *ReportBuilder) ViewSharedReport(c *gin.Context) {
	ctx := c.Request.Context()
	access, report, ok := h.resolveShareToken(c)
	if !ok {
		return
	}

	resp, err := h.buildReportResponse(ctx, report, *access.ShareToken)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	respondSuccess(c, http.StatusOK, resp)
}

// DownloadSharedReport handles
// GET /api/v1/public/reports/share/:token/download/:generation_id. Public.
func (h *ReportBuilder) DownloadSharedReport(c *gin.Context) {
	_, report, ok := h.resolveShareToken(c)
	if !ok {
		return
	}

	generationID, err := uuid.Parse(c.Param("generation_id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid generation id")
		return
	}
	// Scoped to the token's own report, so a token cannot reach another
	// report's artifacts by guessing a generation id.
	h.serveGeneration(c, report.ID, generationID)
}

// ---- helpers ---------------------------------------------------------------

// resolveShareToken loads the access row and report behind a share token,
// writing the error response itself when the token does not resolve.
func (h *ReportBuilder) resolveShareToken(c *gin.Context) (*models.ReportAccess, *models.Report, bool) {
	token := c.Param("token")
	if token == "" {
		respondError(c, http.StatusNotFound, "report not found")
		return nil, nil, false
	}

	ctx := c.Request.Context()
	var access models.ReportAccess
	if err := h.db.WithContext(ctx).First(&access, "share_token = ?", token).Error; err != nil {
		// Deliberately the same response as a missing report: a distinguishable
		// error would let a caller confirm which tokens exist.
		respondError(c, http.StatusNotFound, "report not found")
		return nil, nil, false
	}

	var report models.Report
	if err := h.db.WithContext(ctx).First(&report, "id = ?", access.ReportID).Error; err != nil {
		respondError(c, http.StatusNotFound, "report not found")
		return nil, nil, false
	}
	return &access, &report, true
}

// userCanRead reports whether the user may read the report.
func (h *ReportBuilder) userCanRead(ctx context.Context, report *models.Report, userID uuid.UUID, isAdmin bool) bool {
	if isAdmin || report.CreatedBy == userID || report.UserID == userID {
		return true
	}
	var count int64
	h.db.WithContext(ctx).Model(&models.ReportAccess{}).
		Where("report_id = ? AND user_id = ?", report.ID, userID).
		Count(&count)
	return count > 0
}

// serveGeneration streams a generation's PDF, checking that it belongs to the
// given report before touching the filesystem.
func (h *ReportBuilder) serveGeneration(c *gin.Context, reportID, generationID uuid.UUID) {
	var generation models.ReportGeneration
	if err := h.db.WithContext(c.Request.Context()).
		First(&generation, "id = ? AND report_id = ?", generationID, reportID).Error; err != nil {
		respondError(c, http.StatusNotFound, "report generation not found")
		return
	}

	// GetPDFPath rejects anything that is not a bare file name, so a stored
	// value cannot walk out of the output directory.
	path, err := h.pdfRenderer.GetPDFPath(generation.PDFPath)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := os.Stat(path); err != nil {
		respondError(c, http.StatusNotFound, "report file is no longer available")
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", generation.PDFPath))
	c.Header("Content-Type", "application/pdf")
	c.File(path)
}

// buildReportResponse assembles a report with its template name and generations.
// shareToken, when non-empty, produces public download URLs instead of
// authenticated ones.
func (h *ReportBuilder) buildReportResponse(ctx context.Context, report *models.Report, shareToken string) (ReportResponse, error) {
	var template models.ReportTemplate
	// A deleted template leaves the name blank rather than failing the listing.
	h.db.WithContext(ctx).First(&template, "id = ?", report.TemplateID)

	var generations []models.ReportGeneration
	if err := h.db.WithContext(ctx).Where("report_id = ?", report.ID).
		Order("generated_at DESC").Find(&generations).Error; err != nil {
		return ReportResponse{}, fmt.Errorf("loading report generations: %w", err)
	}

	var lastGenerated *time.Time
	genResponses := make([]ReportGenerationResponse, 0, len(generations))
	for i := range generations {
		gen := generations[i]
		if lastGenerated == nil || gen.GeneratedAt.After(*lastGenerated) {
			t := gen.GeneratedAt
			lastGenerated = &t
		}
		url := downloadURL(report.ID, gen.ID)
		if shareToken != "" {
			url = fmt.Sprintf("/api/v1/public/reports/share/%s/download/%s", shareToken, gen.ID)
		}
		genResponses = append(genResponses, ReportGenerationResponse{
			ID:          gen.ID,
			GeneratedAt: gen.GeneratedAt,
			FileSize:    gen.FileSize,
			DownloadURL: url,
		})
	}

	return ReportResponse{
		ID:            report.ID,
		Name:          report.Name,
		TemplateName:  template.Name,
		ScopeType:     report.ScopeType,
		TimeRangeDays: report.TimeRangeDays,
		CreatedAt:     report.CreatedAt,
		UpdatedAt:     report.UpdatedAt,
		LastGenerated: lastGenerated,
		Generations:   genResponses,
	}, nil
}

func downloadURL(reportID, generationID uuid.UUID) string {
	return fmt.Sprintf("/api/v1/reports/%s/download/%s", reportID, generationID)
}

// generateShareToken returns a 256-bit hex token. The read error is checked:
// ignoring it could yield an all-zero, guessable token for a public link.
func generateShareToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RegisterReportBuilderRoutes mounts the authenticated report-builder endpoints.
func RegisterReportBuilderRoutes(rg *gin.RouterGroup, builder *ReportBuilder) {
	reports := rg.Group("/reports")
	reports.POST("/generate", builder.GenerateReport)
	reports.GET("", builder.ListReports)
	reports.GET("/:id/download/:generation_id", builder.DownloadReport)
	reports.POST("/:id/share", builder.ShareReport)
}

// RegisterPublicReportRoutes mounts the share-token endpoints, which must sit
// outside the /api/v1 group because that group is behind AuthMiddleware. They
// are namespaced under /public to keep them from colliding with the
// authenticated ":id" routes above, which would otherwise conflict in gin's
// router tree.
func RegisterPublicReportRoutes(router *gin.Engine, builder *ReportBuilder) {
	public := router.Group("/api/v1/public/reports/share")
	public.GET("/:token", builder.ViewSharedReport)
	public.GET("/:token/download/:generation_id", builder.DownloadSharedReport)
}
