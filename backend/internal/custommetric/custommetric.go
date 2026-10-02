// Package custommetric turns walked SNMP columns into metric rows (filter,
// precision, scale, labels, used/free percentages, states, counter rates)
// and evaluates a metric's alert rule per row. It is pure: no I/O.
package custommetric

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Value struct {
	Num   float64
	NumOK bool
	Text  string
}

// text is how a value compares with a filter value or reads as a label.
func (v Value) text() string {
	if v.NumOK {
		return strconv.FormatFloat(v.Num, 'f', -1, 64)
	}
	return v.Text
}

type Columns map[string]map[string]Value

type Rule struct {
	Kind    string
	Value   float64
	Hold    time.Duration
	Enabled bool
}

type Definition struct {
	Name, Key, Source, Kind                              string
	Scale                                                float64
	OID, OID2, PrecisionOID, FilterOID                   string
	FilterValues                                         []string
	LabelMode, LabelOID, LabelPointerOID, LabelTargetOID string
	OKStates                                             []int64
	StateNames                                           map[int64]string
	Rule                                                 Rule
}

type Row struct {
	Instance, Label string
	Value           float64
	State           string
	OK              bool
}

// Needed lists the OIDs to read: every run (values) and cached with the
// inventory (filter, labels, precision), each without duplicates.
func Needed(d Definition) (every, cached []string) {
	add := func(list *[]string, oid string) {
		if oid != "" && !slices.Contains(*list, oid) {
			*list = append(*list, oid)
		}
	}
	if d.Source == "scalar" {
		add(&every, d.OID+".0")
		return every, nil
	}
	add(&every, d.OID)
	if d.Source == "used_free_pct" {
		add(&every, d.OID2)
	}
	add(&cached, d.PrecisionOID)
	add(&cached, d.FilterOID)
	switch d.LabelMode {
	case "column", "same_index":
		add(&cached, d.LabelOID)
	case "pointer":
		add(&cached, d.LabelPointerOID)
		add(&cached, d.LabelTargetOID)
	}
	return every, cached
}

// Evaluate returns the metric's rows from the walked columns.
func Evaluate(d Definition, cols Columns) []Row {
	scale := d.Scale
	if scale == 0 {
		scale = 1
	}
	if d.Source == "scalar" {
		v, ok := cols[d.OID+".0"][""]
		if !ok || !v.NumOK {
			return nil
		}
		return []Row{finish(d, Row{Instance: "", Label: d.Name}, v.Num*scale)}
	}
	var rows []Row
	for idx, v := range cols[d.OID] {
		if d.FilterOID != "" {
			fv, ok := cols[d.FilterOID][idx]
			if !ok || !slices.Contains(d.FilterValues, fv.text()) {
				continue
			}
		}
		if !v.NumOK {
			continue
		}
		num := v.Num
		if d.Source == "used_free_pct" {
			free, ok := cols[d.OID2][idx]
			if !ok || !free.NumOK || num+free.Num == 0 {
				continue
			}
			num = num / (num + free.Num) * 100
		}
		if d.PrecisionOID != "" {
			if p, ok := cols[d.PrecisionOID][idx]; ok && p.NumOK {
				num /= math.Pow(10, p.Num)
			}
		}
		rows = append(rows, finish(d, Row{Instance: idx, Label: label(d, cols, idx)}, num*scale))
	}
	sort.Slice(rows, func(i, j int) bool { return LessIndex(rows[i].Instance, rows[j].Instance) })
	return rows
}

func finish(d Definition, r Row, num float64) Row {
	r.Value = num
	if d.Kind == "status" {
		code := int64(num)
		r.State = d.StateNames[code]
		if r.State == "" {
			r.State = strconv.FormatInt(code, 10)
		}
		r.OK = slices.Contains(d.OKStates, code)
	}
	return r
}

func label(d Definition, cols Columns, idx string) string {
	var l string
	switch d.LabelMode {
	case "column", "same_index":
		l = cols[d.LabelOID][idx].text()
	case "pointer":
		if p, ok := cols[d.LabelPointerOID][idx]; ok && p.NumOK && p.Num != 0 {
			l = cols[d.LabelTargetOID][p.text()].text()
		}
	default:
		l = idx
	}
	if l = strings.TrimSpace(l); l == "" {
		l = "Row " + idx
	}
	return l
}

// LessIndex orders row indexes by numeric arcs ("2" before "10", "1.2" before "1.10").
func LessIndex(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, ex := strconv.ParseUint(as[i], 10, 64)
		y, ey := strconv.ParseUint(bs[i], 10, 64)
		if ex != nil || ey != nil {
			if as[i] != bs[i] {
				return as[i] < bs[i]
			}
			continue
		}
		if x != y {
			return x < y
		}
	}
	return len(as) < len(bs)
}

type Sample struct {
	Value float64
	At    time.Time
}

// Rates turns counter rows into per-second rates against the previous
// samples, returning the rate rows and the samples to keep for next time.
func Rates(prev map[string]Sample, rows []Row, at time.Time) ([]Row, map[string]Sample) {
	next := make(map[string]Sample, len(rows))
	var out []Row
	for _, r := range rows {
		next[r.Instance] = Sample{Value: r.Value, At: at}
		p, ok := prev[r.Instance]
		if !ok {
			continue
		}
		dt := at.Sub(p.At).Seconds()
		if dt <= 0 {
			continue
		}
		delta := r.Value - p.Value
		if delta < 0 {
			if p.Value >= 1<<32 {
				continue // a 64-bit counter went backwards: a reboot
			}
			delta += 1 << 32
		}
		r.Value = delta / dt
		out = append(out, r)
	}
	return out, next
}
