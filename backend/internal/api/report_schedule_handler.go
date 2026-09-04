package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// ReportScheduleHandler serves scheduled report delivery.
//
// Every endpoint here authorizes against the parent report. The original design
// checked access only on create, leaving list, update, delete, and manual-run
// open to any authenticated user - which would have let anyone read another
// tenant's recipient addresses, delete their schedules, or trigger mail on
// demand.
type ReportScheduleHandler struct {
	db        *gorm.DB
	scheduler *services.ReportSchedulerService
}

// NewReportScheduleHandler returns a handler bound to db and the scheduler.
func NewReportScheduleHandler(db *gorm.DB, scheduler *services.ReportSchedulerService) *ReportScheduleHandler {
	return &ReportScheduleHandler{db: db, scheduler: scheduler}
}

// scheduleRequest is the create/update body.
type scheduleRequest struct {
	ScheduleType     string   `json:"schedule_type" binding:"required,oneof=daily weekly monthly custom"`
	CronExpression   *string  `json:"cron_expression"`
	EmailRecipients  []string `json:"email_recipients" binding:"required,min=1"`
	SendAsAttachment *bool    `json:"send_as_attachment"`
	IncludeInEmail   *struct {
		IncludeLink    bool `json:"include_link"`
		IncludeSummary bool `json:"include_summary"`
	} `json:"include_in_email"`
	IsActive *bool `json:"is_active"`
}

// scheduleResponse is the API shape of a schedule.
type scheduleResponse struct {
	ID               uuid.UUID              `json:"id"`
	ReportID         uuid.UUID              `json:"report_id"`
	ScheduleType     string                 `json:"schedule_type"`
	CronExpression   *string                `json:"cron_expression"`
	EmailRecipients  []string               `json:"email_recipients"`
	SendAsAttachment bool                   `json:"send_as_attachment"`
	IncludeInEmail   models.EmailInclusions `json:"include_in_email"`
	LastRunAt        *time.Time             `json:"last_run_at"`
	NextRunAt        *time.Time             `json:"next_run_at"`
	IsActive         bool                   `json:"is_active"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

func toScheduleResponse(s models.ReportSchedule) scheduleResponse {
	return scheduleResponse{
		ID: s.ID, ReportID: s.ReportID, ScheduleType: s.ScheduleType,
		CronExpression: s.CronExpression, EmailRecipients: s.EmailRecipients,
		SendAsAttachment: s.SendAsAttachment, IncludeInEmail: s.IncludeInEmail,
		LastRunAt: s.LastRunAt, NextRunAt: s.NextRunAt, IsActive: s.IsActive,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

// authorizeReport loads the report and confirms the caller may administer its
// schedules. Scheduling sends mail on the report's behalf, so it requires
// ownership or admin - viewer access is not enough. On denial it writes the
// response and returns false.
func (h *ReportScheduleHandler) authorizeReport(c *gin.Context, reportID uuid.UUID) (*models.Report, bool) {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondError(c, http.StatusUnauthorized, "authentication required")
		return nil, false
	}

	var report models.Report
	if err := h.db.WithContext(c.Request.Context()).First(&report, "id = ?", reportID).Error; err != nil {
		respondError(c, http.StatusNotFound, "report not found")
		return nil, false
	}
	if !isAdmin && report.CreatedBy != userID && report.UserID != userID {
		respondError(c, http.StatusForbidden, "you do not have permission to schedule this report")
		return nil, false
	}
	return &report, true
}

// loadAuthorizedSchedule resolves a schedule id and authorizes via its report.
func (h *ReportScheduleHandler) loadAuthorizedSchedule(c *gin.Context) (*models.ReportSchedule, bool) {
	scheduleID, err := uuid.Parse(c.Param("schedule_id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid schedule id")
		return nil, false
	}

	var schedule models.ReportSchedule
	if err := h.db.WithContext(c.Request.Context()).First(&schedule, "id = ?", scheduleID).Error; err != nil {
		respondError(c, http.StatusNotFound, "schedule not found")
		return nil, false
	}
	if _, ok := h.authorizeReport(c, schedule.ReportID); !ok {
		return nil, false
	}
	return &schedule, true
}

// applyRequest maps a request body onto a schedule and validates the result.
func applyRequest(schedule *models.ReportSchedule, req scheduleRequest) error {
	schedule.ScheduleType = req.ScheduleType
	schedule.CronExpression = req.CronExpression

	recipients := make(models.StringSlice, 0, len(req.EmailRecipients))
	for _, r := range req.EmailRecipients {
		if trimmed := strings.TrimSpace(r); trimmed != "" {
			recipients = append(recipients, trimmed)
		}
	}
	schedule.EmailRecipients = recipients

	// Attachments default to on, matching the column default; an explicit false
	// is honoured.
	schedule.SendAsAttachment = true
	if req.SendAsAttachment != nil {
		schedule.SendAsAttachment = *req.SendAsAttachment
	}
	if req.IncludeInEmail != nil {
		schedule.IncludeInEmail = models.EmailInclusions{
			IncludeLink:    req.IncludeInEmail.IncludeLink,
			IncludeSummary: req.IncludeInEmail.IncludeSummary,
		}
	}
	if req.IsActive != nil {
		schedule.IsActive = *req.IsActive
	}

	if err := schedule.Validate(); err != nil {
		return err
	}
	// Reject a bad cron expression here rather than logging it at registration
	// and leaving a schedule that silently never runs.
	expr, err := services.CronExpressionFor(schedule.ScheduleType, schedule.CronExpression)
	if err != nil {
		return err
	}
	return services.ValidateCronExpression(expr)
}

// CreateSchedule handles POST /api/v1/reports/:id/schedules.
func (h *ReportScheduleHandler) CreateSchedule(c *gin.Context) {
	reportID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid report id")
		return
	}
	report, ok := h.authorizeReport(c, reportID)
	if !ok {
		return
	}
	userID, _, _, _ := GetUserFromContext(c)

	var req scheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	schedule := models.ReportSchedule{
		ID:       uuid.New(),
		ReportID: report.ID,
		UserID:   userID,
		IsActive: true,
	}
	if err := applyRequest(&schedule, req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.db.WithContext(c.Request.Context()).Create(&schedule).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "creating schedule: "+err.Error())
		return
	}
	if schedule.IsActive {
		if err := h.scheduler.Register(&schedule); err != nil {
			respondError(c, http.StatusInternalServerError, "registering schedule: "+err.Error())
			return
		}
	}

	// Reload so next_run_at, set during registration, is in the response.
	h.db.WithContext(c.Request.Context()).First(&schedule, "id = ?", schedule.ID)
	respondSuccess(c, http.StatusCreated, toScheduleResponse(schedule))
}

// ListSchedules handles GET /api/v1/reports/:id/schedules.
func (h *ReportScheduleHandler) ListSchedules(c *gin.Context) {
	reportID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid report id")
		return
	}
	// Recipient addresses are in this payload, so listing is authorized too.
	if _, ok := h.authorizeReport(c, reportID); !ok {
		return
	}

	var schedules []models.ReportSchedule
	if err := h.db.WithContext(c.Request.Context()).
		Where("report_id = ?", reportID).Order("created_at DESC").Find(&schedules).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "listing schedules: "+err.Error())
		return
	}

	out := make([]scheduleResponse, 0, len(schedules))
	for _, s := range schedules {
		out = append(out, toScheduleResponse(s))
	}
	respondSuccess(c, http.StatusOK, out)
}

// UpdateSchedule handles PATCH /api/v1/reports/schedules/:schedule_id.
func (h *ReportScheduleHandler) UpdateSchedule(c *gin.Context) {
	schedule, ok := h.loadAuthorizedSchedule(c)
	if !ok {
		return
	}

	var req scheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := applyRequest(schedule, req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	schedule.UpdatedAt = time.Now()

	if err := h.db.WithContext(c.Request.Context()).Save(schedule).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "updating schedule: "+err.Error())
		return
	}

	// Re-register so the change takes effect now. Without this the cron runner
	// would keep executing the previous cadence and recipient list.
	if schedule.IsActive {
		if err := h.scheduler.Register(schedule); err != nil {
			respondError(c, http.StatusInternalServerError, "re-registering schedule: "+err.Error())
			return
		}
	} else {
		h.scheduler.Unregister(schedule.ID)
	}

	h.db.WithContext(c.Request.Context()).First(schedule, "id = ?", schedule.ID)
	respondSuccess(c, http.StatusOK, toScheduleResponse(*schedule))
}

// DeleteSchedule handles DELETE /api/v1/reports/schedules/:schedule_id.
func (h *ReportScheduleHandler) DeleteSchedule(c *gin.Context) {
	schedule, ok := h.loadAuthorizedSchedule(c)
	if !ok {
		return
	}

	if err := h.db.WithContext(c.Request.Context()).
		Delete(&models.ReportSchedule{}, "id = ?", schedule.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "deleting schedule: "+err.Error())
		return
	}
	// Drop the cron job too, or a deleted schedule keeps sending mail.
	h.scheduler.Unregister(schedule.ID)

	respondSuccess(c, http.StatusOK, gin.H{"deleted": schedule.ID})
}

// RunScheduleNow handles POST /api/v1/reports/schedules/:schedule_id/run.
//
// It runs synchronously so the caller learns whether delivery actually worked.
// The original design fired a goroutine and returned 202 immediately, which
// reports success even when the email fails.
func (h *ReportScheduleHandler) RunScheduleNow(c *gin.Context) {
	schedule, ok := h.loadAuthorizedSchedule(c)
	if !ok {
		return
	}

	if err := h.scheduler.RunSchedule(c.Request.Context(), schedule.ID); err != nil {
		respondError(c, http.StatusInternalServerError, "running schedule: "+err.Error())
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{
		"schedule_id": schedule.ID,
		"recipients":  len(schedule.EmailRecipients),
		"message":     "report generated and delivered",
	})
}

// RegisterReportScheduleRoutes mounts the schedule endpoints at the URLs the
// feature spec defines.
//
// The nested parameter is named ":id", not the spec's ":report_id". gin keeps a
// separate radix tree per HTTP method, and the POST tree already holds
// "/reports/:id/share" from the report-builder routes; a second, differently
// named wildcard in that position panics at startup with
//
//	':report_id' ... conflicts with existing wildcard ':id'
//
// The URLs are unchanged - only the parameter name is. The static
// "/reports/schedules/..." segment below is a legitimate sibling of that
// wildcard and needs no such adjustment.
func RegisterReportScheduleRoutes(rg *gin.RouterGroup, h *ReportScheduleHandler) {
	rg.POST("/reports/:id/schedules", h.CreateSchedule)
	rg.GET("/reports/:id/schedules", h.ListSchedules)

	schedules := rg.Group("/reports/schedules")
	schedules.PATCH("/:schedule_id", h.UpdateSchedule)
	schedules.DELETE("/:schedule_id", h.DeleteSchedule)
	schedules.POST("/:schedule_id/run", h.RunScheduleNow)
}
