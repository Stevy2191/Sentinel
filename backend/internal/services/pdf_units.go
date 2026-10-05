package services

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Number formatting for Metrics reports, in the PDF and the email summary.
// Rates and sizes are decimal (1 Mbps = 1,000,000 bps, 1 GB = 10^9 bytes),
// the way carriers bill. Every function prints "-" for NaN or infinity
// rather than "NaN" on a customer's report.

var (
	bpsUnits  = []string{"bps", "Kbps", "Mbps", "Gbps"}
	byteUnits = []string{"B", "KB", "MB", "GB", "TB", "PB"}
)

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// trimZeros drops trailing zeros after a decimal point ("2.10" -> "2.1",
// "40.0" -> "40"), and a negative zero.
func trimZeros(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		return "0"
	}
	return s
}

// formatSig prints v with about three significant digits: 640, 12.5, 2.1, 0.25.
// Below 0.01 it keeps two significant digits (0.004, 0.00049), so a small
// rate such as errors per minute never prints as "0"; exact zero is "0".
func formatSig(v float64) string {
	if !finite(v) {
		return "-"
	}
	prec := 2
	switch a := math.Abs(v); {
	case a >= 99.95:
		prec = 0
	case a >= 9.995:
		prec = 1
	case a > 0 && a < 0.01:
		// The first significant digit is the -floor(log10 a)th decimal.
		prec = 1 - int(math.Floor(math.Log10(a)))
	}
	return trimZeros(strconv.FormatFloat(v, 'f', prec, 64))
}

// scaleDecimal steps v up through units in thousands. A value that would
// print as 1000 of one unit moves to the next (999.6 Mbps is "1 Gbps").
func scaleDecimal(v float64, units []string) string {
	if !finite(v) {
		return "-"
	}
	i := 0
	for i < len(units)-1 && math.Abs(v) >= 999.5 {
		v /= 1000
		i++
	}
	return formatSig(v) + " " + units[i]
}

// formatBps prints a rate: "950 bps", "12.3 Kbps", "640 Mbps", "1.25 Gbps".
func formatBps(v float64) string { return scaleDecimal(v, bpsUnits) }

// formatBytes prints a byte total: "512 KB", "1.5 MB", "2.1 TB".
func formatBytes(v float64) string { return scaleDecimal(v, byteUnits) }

// formatCount prints a whole count with thousands separators: "1,234,567".
func formatCount(v float64) string {
	if !finite(v) {
		return "-"
	}
	n := int64(math.Round(v))
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return sign + b.String()
}

// formatPercent prints a percentage with one decimal at most: "12.3%", "40%".
func formatPercent(p float64) string {
	if !finite(p) {
		return "-"
	}
	return trimZeros(strconv.FormatFloat(p, 'f', 1, 64)) + "%"
}

// formatDurationHM prints a length of time in hours and minutes: "2 h 13 m",
// "45 m", "1 h". Under half a minute is "< 1 m", nothing at all "0 m".
func formatDurationHM(seconds float64) string {
	if !finite(seconds) || seconds <= 0 {
		return "0 m"
	}
	mins := int64(math.Round(seconds / 60))
	h, m := mins/60, mins%60
	switch {
	case mins == 0:
		return "< 1 m"
	case h == 0:
		return fmt.Sprintf("%d m", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d m", h, m)
}

// formatValue prints a statistic in its metric's unit. bps scales to
// Kbps/Mbps/Gbps; per_min is a rate per minute; bool is the share of time
// true (its average, 0-1); %, °C, V, min and custom units print verbatim
// after the number.
func formatValue(v float64, unit string) string {
	if !finite(v) {
		return "-"
	}
	switch unit {
	case "bps":
		return formatBps(v)
	case "per_min":
		return formatSig(v) + "/min"
	case "%":
		return formatPercent(v)
	case "bool":
		return formatPercent(v * 100)
	case "":
		return formatSig(v)
	}
	return formatSig(v) + " " + unit
}

// formatTotal prints a row's total: bytes for bps, a count for per_min.
// Other units have no total and print "".
func formatTotal(v float64, unit string) string {
	switch unit {
	case "bps":
		return formatBytes(v)
	case "per_min":
		return formatCount(v)
	}
	return ""
}

// formatChange prints a change on the previous period: "+18%", "-3.5%",
// "0%", or "new" when there was nothing to compare with. A missing change
// is "".
func formatChange(change *float64, isNew bool) string {
	if isNew {
		return "new"
	}
	if change == nil || !finite(*change) {
		return ""
	}
	prec := 0
	if math.Abs(*change) < 9.95 {
		prec = 1
	}
	s := trimZeros(strconv.FormatFloat(*change, 'f', prec, 64))
	if s == "0" {
		return "0%"
	}
	if !strings.HasPrefix(s, "-") {
		s = "+" + s
	}
	return s + "%"
}
