package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
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
	// scheduler is used when deleting a report: the database cascades its
	// schedules away, but their cron jobs would otherwise keep firing against
	// rows that no longer exist.
	scheduler *services.ReportSchedulerService
}

// NewReportBuilder returns a handler set bound to its dependencies.
func NewReportBuilder(
	db *gorm.DB,
	aggregator *services.ReportAggregatorService,
	pdfRenderer *services.PDFRendererService,
	scheduler *services.ReportSchedulerService,
) *ReportBuilder {
	return &ReportBuilder{
		db:            db,
		aggregator:    aggregator,
		pdfRenderer:   pdfRenderer,
		htmlGenerator: services.NewHTMLReportGenerator(),
		scheduler:     scheduler,
	}
}

// SetScheduler wires the report scheduler in after construction, breaking the
// cycle between the builder and the scheduler (each needs the other).
func (h *ReportBuilder) SetScheduler(s *services.ReportSchedulerService) {
	h.scheduler = s
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
		respondInternal(c, "loading report template", err)
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
		respondInternal(c, "aggregating report data", err)
		return
	}

	pdfFilename, err := h.pdfRenderer.RenderReportToPDF(
		reportData, template.Sections, "report_"+report.ID.String()[:8])
	if err != nil {
		respondInternal(c, "rendering report PDF", err)
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
		respondInternal(c, "saving report", err)
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

	// Bounded: an install that has accumulated thousands of reports must not be
	// able to make this one query load them all into memory.
	limit, offset := paginationParams(c)

	var total int64
	query.Count(&total)

	var reports []models.Report
	if err := query.Order("created_at DESC").Limit(limit).Offset(offset).
		Find(&reports).Error; err != nil {
		respondInternal(c, "listing reports", err)
		return
	}

	// Always a slice, never null - the frontend maps over this directly.
	out := make([]ReportResponse, 0, len(reports))
	for i := range reports {
		resp, err := h.buildReportResponse(ctx, &reports[i], "")
		if err != nil {
			respondInternal(c, "ListReports", err)
			return
		}
		out = append(out, resp)
	}
	// The payload stays a bare array so existing callers keep working; the totals
	// go in headers rather than changing the response shape.
	c.Header("X-Total-Count", strconv.FormatInt(total, 10))
	c.Header("X-Limit", strconv.Itoa(limit))
	c.Header("X-Offset", strconv.Itoa(offset))
	respondSuccess(c, http.StatusOK, out)
}

// paginationParams reads ?limit and ?offset, clamped to a sane range.
func paginationParams(c *gin.Context) (limit, offset int) {
	limit = 50
	if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > 100 {
		limit = 100
	}
	if v, err := strconv.Atoi(c.Query("offset")); err == nil && v > 0 {
		offset = v
	}
	return limit, offset
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
		respondInternal(c, "generating share token", err)
		return
	}

	access := models.ReportAccess{
		ID:         uuid.New(),
		ReportID:   report.ID,
		AccessType: models.AccessTypeViewer,
		ShareToken: &token,
	}
	if err := h.db.WithContext(ctx).Create(&access).Error; err != nil {
		respondInternal(c, "creating share link", err)
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
		respondInternal(c, "ViewSharedReport", err)
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
		respondInternal(c, "serveGeneration", err)
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

// ListTemplates handles GET /api/v1/report-templates. The report builder needs
// this to offer a template choice; without it the wizard has nothing to select.
func (h *ReportBuilder) ListTemplates(c *gin.Context) {
	var templates []models.ReportTemplate
	if err := h.db.WithContext(c.Request.Context()).
		Order("is_default DESC, name ASC").Find(&templates).Error; err != nil {
		respondInternal(c, "listing report templates", err)
		return
	}
	if templates == nil {
		templates = []models.ReportTemplate{}
	}
	respondSuccess(c, http.StatusOK, templates)
}

// ListMonitorTags handles GET /api/v1/monitor-tags, returning every distinct tag
// in use. The report builder scopes reports by tag, and tags live in a JSONB
// column on monitors rather than a table of their own.
func (h *ReportBuilder) ListMonitorTags(c *gin.Context) {
	var tags []string
	if err := h.db.WithContext(c.Request.Context()).
		Raw(`SELECT DISTINCT jsonb_array_elements_text(tags) AS tag
		       FROM monitors
		      WHERE tags IS NOT NULL AND jsonb_typeof(tags) = 'array'
		      ORDER BY tag`).Scan(&tags).Error; err != nil {
		respondInternal(c, "listing monitor tags", err)
		return
	}
	if tags == nil {
		tags = []string{}
	}
	respondSuccess(c, http.StatusOK, tags)
}

// DeleteReport handles DELETE /api/v1/reports/:id. It removes the definition,
// its generations, access grants, and schedules, and deletes the rendered PDFs
// from disk so nothing is left orphaned.
func (h *ReportBuilder) DeleteReport(c *gin.Context) {
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
	// Deleting is an owner-level action; read access is not enough.
	if !isAdmin && report.CreatedBy != userID && report.UserID != userID {
		respondError(c, http.StatusForbidden, "only the report owner can delete it")
		return
	}

	// Collect what has to be cleaned up outside the database before the rows go.
	var generations []models.ReportGeneration
	h.db.WithContext(ctx).Where("report_id = ?", reportID).Find(&generations)

	var schedules []models.ReportSchedule
	h.db.WithContext(ctx).Where("report_id = ?", reportID).Find(&schedules)

	if err := h.db.WithContext(ctx).Delete(&models.Report{}, "id = ?", reportID).Error; err != nil {
		respondInternal(c, "deleting report", err)
		return
	}

	// The database cascades the schedule rows, but their cron jobs live in this
	// process and would keep firing against rows that no longer exist.
	if h.scheduler != nil {
		for _, s := range schedules {
			h.scheduler.Unregister(s.ID)
		}
	}

	// Best effort: a file that cannot be removed is logged through the response
	// rather than failing a delete that has already committed.
	removed, failed := 0, 0
	for _, g := range generations {
		if err := h.pdfRenderer.DeletePDF(g.PDFPath); err != nil {
			failed++
			continue
		}
		removed++
	}

	respondSuccess(c, http.StatusOK, gin.H{
		"deleted":           report.ID,
		"schedules_removed": len(schedules),
		"files_removed":     removed,
		"files_not_removed": failed,
	})
}

// RegisterReportBuilderRoutes mounts the authenticated report-builder endpoints.
func RegisterReportBuilderRoutes(rg *gin.RouterGroup, builder *ReportBuilder) {
	reports := rg.Group("/reports")
	// Each generation aggregates a window and renders a PDF, so it is the most
	// expensive authenticated call in the API: 5 a minute per user.
	generateLimit := NewRateLimiter(5, time.Minute, 5).Middleware("report-generate", ByUser)
	reports.POST("/generate", generateLimit, builder.GenerateReport)
	reports.GET("", builder.ListReports)
	reports.GET("/:id/download/:generation_id", builder.DownloadReport)
	reports.POST("/:id/share", builder.ShareReport)
	reports.DELETE("/:id", builder.DeleteReport)

	// Sibling resources the builder UI needs. They sit outside the /reports
	// group so they do not collide with its ":id" wildcard.
	rg.GET("/report-templates", builder.ListTemplates)
	rg.GET("/monitor-tags", builder.ListMonitorTags)
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
