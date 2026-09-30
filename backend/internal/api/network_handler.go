package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type metricsQuerier interface {
	Query(ctx context.Context, q services.MetricsQuery) (*services.MetricsResult, error)
}

type networkSettingsStore interface {
	Get(ctx context.Context) services.NetworkSettings
	Update(ctx context.Context, p services.NetworkSettingsPatch) (services.NetworkSettings, error)
}

// RegisterNetworkRoutes mounts /network: the generic metrics query (any
// signed-in user, filtered by site access) and the network settings (admin).
func RegisterNetworkRoutes(rg *gin.RouterGroup, devices deviceStore, ports portStore, metrics metricsQuerier,
	settings networkSettingsStore, sites siteAccessChecker, users adminChecker) {
	g := rg.Group("/network")
	g.GET("/metrics/query", metricsQueryHandler(devices, ports, metrics, sites))
	admin := g.Group("", RequireAdmin(users))
	admin.GET("/settings", func(c *gin.Context) { respondSuccess(c, http.StatusOK, settings.Get(c.Request.Context())) })
	admin.PATCH("/settings", updateNetworkSettingsHandler(settings))
}

var queryRanges = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour, "1y": 365 * 24 * time.Hour,
}

const (
	maxQueryMetrics   = 10
	maxQueryDevices   = 50
	maxQueryInstances = 200
	maxQuerySpan      = 400 * 24 * time.Hour
)

// canSeeSite reports whether the caller has at least readonly access,
// without writing a response.
func canSeeSite(c *gin.Context, sites siteAccessChecker, siteID uuid.UUID) (bool, error) {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		return false, nil
	}
	level, err := sites.SiteAccess(c.Request.Context(), userID, isAdmin, siteID)
	if errors.Is(err, services.ErrSiteNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return level != services.SiteAccessNone, nil
}

// metricsQueryHandler handles GET /network/metrics/query:
//
//	metric=... (1-10, repeatable), device_id=... (1-50, repeatable) or
//	site_id=... (with agg=sum), instance=... (optional, repeatable),
//	range=1h|6h|24h|7d|30d|90d|1y or from=&to= (RFC 3339), agg=sum,
//	ports=physical (sum only physical ports, not link aggregates).
func metricsQueryHandler(devices deviceStore, ports portStore, metrics metricsQuerier, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		names := c.QueryArray("metric")
		if len(names) == 0 || len(names) > maxQueryMetrics {
			respondError(c, http.StatusBadRequest, "choose 1 to 10 metrics")
			return
		}
		for _, n := range names {
			if !services.KnownMetric(n) {
				respondError(c, http.StatusBadRequest, fmt.Sprintf("unknown metric %q", n))
				return
			}
		}
		instances := c.QueryArray("instance")
		if len(instances) > maxQueryInstances {
			respondError(c, http.StatusBadRequest, "at most 200 instances")
			return
		}
		to := time.Now().UTC()
		var from time.Time
		if r := c.Query("range"); r != "" {
			span, ok := queryRanges[r]
			if !ok {
				respondError(c, http.StatusBadRequest, "range must be 1h, 6h, 24h, 7d, 30d, 90d or 1y")
				return
			}
			from = to.Add(-span)
		} else {
			var err1, err2 error
			from, err1 = time.Parse(time.RFC3339, c.Query("from"))
			to, err2 = time.Parse(time.RFC3339, c.Query("to"))
			if err1 != nil || err2 != nil || !from.Before(to) || to.Sub(from) > maxQuerySpan {
				respondError(c, http.StatusBadRequest, "give range, or from and to (RFC 3339, from before to, at most 400 days)")
				return
			}
		}
		agg := c.Query("agg")
		if agg != "" && agg != "sum" {
			respondError(c, http.StatusBadRequest, "agg must be sum")
			return
		}

		var ids []uuid.UUID
		deviceParams := c.QueryArray("device_id")
		if raw := c.Query("site_id"); raw != "" {
			if len(deviceParams) > 0 {
				respondError(c, http.StatusBadRequest, "use site_id or device_id, not both")
				return
			}
			if agg != "sum" {
				respondError(c, http.StatusBadRequest, "a site query needs agg=sum")
				return
			}
			siteID, err := uuid.Parse(raw)
			if err != nil {
				respondError(c, http.StatusBadRequest, "site_id must be a UUID")
				return
			}
			if !requireSiteLevel(c, sites, siteID, services.SiteAccessReadonly) {
				return
			}
			userID, _, isAdmin, _ := GetUserFromContext(c)
			list, err := devices.List(ctx, userID, isAdmin, services.DeviceFilter{SiteID: &siteID})
			if err != nil {
				respondInternal(c, "metricsQuery", err)
				return
			}
			for _, d := range list {
				ids = append(ids, d.ID)
			}
		} else {
			if len(deviceParams) == 0 || len(deviceParams) > maxQueryDevices {
				respondError(c, http.StatusBadRequest, "give 1 to 50 device_id values, or a site_id")
				return
			}
			for _, raw := range deviceParams {
				id, err := uuid.Parse(raw)
				if err != nil {
					respondError(c, http.StatusBadRequest, "device_id must be a UUID")
					return
				}
				d, err := devices.Get(ctx, id)
				if errors.Is(err, services.ErrDeviceNotFound) {
					respondError(c, http.StatusNotFound, "device not found")
					return
				}
				if err != nil {
					respondInternal(c, "metricsQuery", err)
					return
				}
				ok, err := canSeeSite(c, sites, d.SiteID)
				if err != nil {
					respondInternal(c, "metricsQuery", err)
					return
				}
				if !ok {
					respondError(c, http.StatusNotFound, "device not found")
					return
				}
				ids = append(ids, id)
			}
		}

		q := services.MetricsQuery{DeviceIDs: ids, Metrics: names, Instances: instances, From: from, To: to, Sum: agg == "sum"}
		if c.Query("ports") == "physical" && len(ids) > 0 {
			ifs, err := ports.PhysicalInterfaceIDs(ctx, ids)
			if err != nil {
				respondInternal(c, "metricsQuery", err)
				return
			}
			if len(ifs) == 0 {
				ifs = []uuid.UUID{uuid.Nil} // matches no series
			}
			q.InterfaceIDs = ifs
		}
		res, err := metrics.Query(ctx, q)
		if err != nil {
			respondInternal(c, "metricsQuery", err)
			return
		}
		respondSuccess(c, http.StatusOK, res)
	}
}

func updateNetworkSettingsHandler(settings networkSettingsStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var p services.NetworkSettingsPatch
		if err := c.ShouldBindJSON(&p); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		s, err := settings.Update(c.Request.Context(), p)
		if err != nil {
			if isInternal(err) {
				respondInternal(c, "updateNetworkSettings", err)
				return
			}
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, s)
	}
}
