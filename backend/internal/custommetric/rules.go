package custommetric

import "time"

type RuleState struct {
	Active map[string]time.Time
	Since  map[string]time.Time
}

type Change struct {
	Instance, Label string
	Started         bool
	At              time.Time
	Value           float64
	State           string
}

// Violates reports whether a row breaks the rule.
func Violates(r Rule, kind string, row Row) bool {
	switch r.Kind {
	case "not_ok":
		return kind == "status" && !row.OK
	case "above":
		return row.Value > r.Value
	case "below":
		return row.Value < r.Value
	}
	return false
}

// EvalRule applies one poll's rows to the rule. A row absent this poll keeps
// its state. prev is not modified.
func EvalRule(r Rule, kind string, prev RuleState, rows []Row, now time.Time) (RuleState, []Change) {
	next := RuleState{Active: map[string]time.Time{}, Since: map[string]time.Time{}}
	for k, v := range prev.Active {
		next.Active[k] = v
	}
	for k, v := range prev.Since {
		next.Since[k] = v
	}
	if !r.Enabled || r.Kind == "" {
		return next, nil
	}
	var changes []Change
	for _, row := range rows {
		ch := Change{Instance: row.Instance, Label: row.Label, At: now, Value: row.Value, State: row.State}
		_, active := next.Active[row.Instance]
		if Violates(r, kind, row) {
			since, held := next.Since[row.Instance]
			if !held {
				since = now
				next.Since[row.Instance] = since
			}
			if !active && now.Sub(since) >= r.Hold {
				next.Active[row.Instance] = now
				ch.Started = true
				changes = append(changes, ch)
			}
			continue
		}
		delete(next.Since, row.Instance)
		if active {
			delete(next.Active, row.Instance)
			changes = append(changes, ch)
		}
	}
	return next, changes
}
