package dashboards

import "time"

// rangeSpans are the ranges a time-based widget may show: the same set the
// metrics query endpoint accepts.
var rangeSpans = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour, "1y": 365 * 24 * time.Hour,
}

// statusRefresh is how often live-status widgets refresh.
const statusRefresh = 30 * time.Second

// ValidRange reports whether r is a known range key.
func ValidRange(r string) bool {
	_, ok := rangeSpans[r]
	return ok
}

// validateRange defaults an empty range to 24h and rejects unknown ones.
func validateRange(r string) (string, error) {
	if r == "" {
		return "24h", nil
	}
	if !ValidRange(r) {
		return "", fieldErr("range", "must be 1h, 6h, 24h, 7d, 30d, 90d or 1y")
	}
	return r, nil
}

// rangeSpan is r's length; an unknown key reads as 24h.
func rangeSpan(r string) time.Duration {
	if d, ok := rangeSpans[r]; ok {
		return d
	}
	return 24 * time.Hour
}

// effectiveRange is the dashboard override when set, else the saved range.
func effectiveRange(saved, override string) string {
	if override != "" {
		return override
	}
	return saved
}

// chartRefresh is the refresh period of a chart or stat showing range r:
// 60 s up to 6h, 5 min up to 7d, 15 min beyond (spec §4).
func chartRefresh(r string) time.Duration {
	switch span := rangeSpan(r); {
	case span <= 6*time.Hour:
		return time.Minute
	case span <= 7*24*time.Hour:
		return 5 * time.Minute
	}
	return 15 * time.Minute
}
