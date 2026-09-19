package services

import (
	"fmt"
	"strings"
)

// The HTML report renders the same sections as the PDF. They are kept in step
// deliberately: a shared report link and its downloaded PDF describing the same
// period differently is worse than either being slightly plainer.

func (g *HTMLReportGenerator) executiveSummarySection(data *ReportData) string {
	var b strings.Builder
	b.WriteString("<h2>Executive Summary</h2>\n")

	current := summarize(data.Metrics)
	prev := data.Previous

	rows := [][2]string{
		{"Availability", formatUptimePercent(current.uptime)},
		{"Incidents", fmt.Sprintf("%d", current.incidents)},
		{"Total downtime", formatMinutes(current.downtime)},
		{"Services covered", fmt.Sprintf("%d", len(data.Metrics))},
	}
	deltas := []string{"", "", "", ""}
	if prev != nil {
		deltas[0] = signedDelta(prev.UptimeDelta, "pp", true)
		deltas[1] = signedCountDelta(prev.IncidentDelta)
		deltas[2] = signedDeltaMinutes(prev.DowntimeDelta)
	}

	b.WriteString("<table>\n<thead><tr><th>Measure</th><th>This period</th><th>Change</th></tr></thead>\n<tbody>\n")
	for i, r := range rows {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			esc(r[0]), esc(r[1]), esc(deltas[i]))
	}
	b.WriteString("</tbody>\n</table>\n")

	if prev != nil {
		fmt.Fprintf(&b, "<p><em>Compared with %s.</em></p>\n", esc(prev.Label))
	}
	if worst, ok := worstMonitor(data.Metrics); ok && worst.Uptime < 100 {
		fmt.Fprintf(&b, "<p><strong>Needs attention:</strong> %s at %s over %s of downtime across %d incident(s).</p>\n",
			esc(worst.MonitorName), esc(formatUptimePercent(worst.Uptime)),
			esc(formatMinutes(worst.DowntimeMinutes)), worst.IncidentCount)
	}
	if perfect := perfectMonitors(data.Metrics); len(perfect) > 0 {
		fmt.Fprintf(&b, "<p><strong>No recorded downtime:</strong> %s.</p>\n", esc(joinCapped(perfect, 8)))
	}
	return b.String()
}

func (g *HTMLReportGenerator) timelineSection(data *ReportData) string {
	var b strings.Builder
	b.WriteString("<h2>What Happened</h2>\n")
	if len(data.Timeline) == 0 {
		b.WriteString("<p>Nothing was recorded during this period.</p>\n")
		return b.String()
	}

	loc := data.ReportLocation()
	b.WriteString("<table>\n<thead><tr><th>When</th><th>Service</th><th>Duration</th><th>Status</th><th>Cause</th></tr></thead>\n<tbody>\n")
	shown := data.Timeline
	if len(shown) > maxTimelineRows {
		shown = shown[:maxTimelineRows]
	}
	for _, e := range shown {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>\n",
			esc(e.StartTime.In(loc).Format("Jan 02 15:04")),
			esc(e.MonitorName),
			esc(formatMinutes(e.DurationMinutes)),
			esc(e.Status),
			esc(e.Cause))
	}
	b.WriteString("</tbody>\n</table>\n")
	if len(data.Timeline) > maxTimelineRows {
		fmt.Fprintf(&b, "<p><em>and %d more incidents in this period</em></p>\n",
			len(data.Timeline)-maxTimelineRows)
	}
	return b.String()
}

func (g *HTMLReportGenerator) availabilitySection(data *ReportData) string {
	var b strings.Builder
	b.WriteString("<h2>Availability Breakdown</h2>\n")
	if len(data.Availability) == 0 {
		b.WriteString("<p>No availability data for this period.</p>\n")
		return b.String()
	}

	b.WriteString("<table>\n<thead><tr><th>Period</th><th>Uptime</th><th>Downtime</th><th>Incidents</th></tr></thead>\n<tbody>\n")
	shown := data.Availability
	if len(shown) > maxAvailabilityRows {
		shown = shown[:maxAvailabilityRows]
	}
	for _, d := range shown {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%d</td></tr>\n",
			esc(d.Label), esc(formatUptimePercent(d.Uptime)),
			esc(formatMinutes(d.DowntimeMinutes)), d.IncidentCount)
	}
	b.WriteString("</tbody>\n</table>\n")
	return b.String()
}

func (g *HTMLReportGenerator) performanceSection(data *ReportData) string {
	var b strings.Builder
	b.WriteString("<h2>Response Time</h2>\n")
	if len(data.Performance) == 0 {
		b.WriteString("<p>No successful checks were recorded for these services during this period.</p>\n")
		return b.String()
	}

	b.WriteString("<table>\n<thead><tr><th>Service</th><th>Average</th><th>95th pct</th><th>Slowest</th><th>Samples</th></tr></thead>\n<tbody>\n")
	shown := data.Performance
	if len(shown) > maxPerformanceRows {
		shown = shown[:maxPerformanceRows]
	}
	for _, p := range shown {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td></tr>\n",
			esc(p.MonitorName), esc(formatMillis(p.AvgMs)), esc(formatMillis(p.P95Ms)),
			esc(formatMillis(float64(p.MaxMs))), p.Samples)
	}
	b.WriteString("</tbody>\n</table>\n")
	b.WriteString("<p><em>Measured from successful checks only. A timed-out check records the timeout setting rather than the service's speed, so including it would turn an outage into a slowdown.</em></p>\n")
	return b.String()
}
