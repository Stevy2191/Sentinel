package services

import (
	"context"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// Names for the three metrics a resource threshold can watch. Used both as
// the AgentThresholdChange.Metric value and, in main.go, as the key into a
// per-metric human label for the alert text.
const (
	ThresholdMetricCPU    = "cpu"
	ThresholdMetricMemory = "memory"
	ThresholdMetricDisk   = "disk"
)

// AgentThresholdChange describes a resource metric crossing its configured
// alert threshold, in either direction.
type AgentThresholdChange struct {
	Agent models.Agent
	// Metric is one of the ThresholdMetric* constants.
	Metric string
	// Value is the sample that caused the transition.
	Value float64
	// Threshold is the configured value it crossed.
	Threshold int
	// Breached is true when the metric just crossed at-or-over the
	// threshold, false when it just dropped back under it.
	Breached bool
}

// SetThresholdChangeHook registers a callback invoked when a metric crosses
// its configured alert threshold, in either direction.
//
// A hook rather than a notification manager dependency, for the same reason
// SetStatusChangeHook is one: this service's job is agent state, and wiring
// delivery into it would pull the whole notification graph in behind it.
// main decides what a transition means.
func (s *AgentService) SetThresholdChangeHook(fn func(context.Context, AgentThresholdChange)) {
	s.onThresholdChange = fn
}

func (s *AgentService) notifyThresholdChange(ctx context.Context, change AgentThresholdChange) {
	if s.onThresholdChange == nil {
		return
	}
	s.onThresholdChange(ctx, change)
}

// thresholdTransition is what evaluating one metric against its configured
// threshold decided.
type thresholdTransition struct {
	// newActive is what the agent's *AlertActive column should become.
	newActive bool
	// notify is true only when this cycle's result differs from the stored
	// flag - a real transition, not a repeat of the current state.
	notify bool
	// breached is the direction of the notification when notify is true:
	// true means "just crossed over", false means "just recovered".
	breached bool
}

// evaluateThreshold decides whether a metric's alert-active flag should
// change, given the incoming sample, the configured threshold (nil means
// not configured), and the flag's current value.
//
// A nil value - a reading that was not reported this cycle, which every
// AgentMetric field allows for exactly this reason - leaves the current
// state exactly as it is: a missing sample must never look like a sudden
// drop to 0% and silently recover an open alert.
func evaluateThreshold(value *float64, threshold *int, currentlyActive bool) thresholdTransition {
	if value == nil || threshold == nil {
		return thresholdTransition{newActive: currentlyActive}
	}
	breached := *value >= float64(*threshold)
	switch {
	case breached && !currentlyActive:
		return thresholdTransition{newActive: true, notify: true, breached: true}
	case !breached && currentlyActive:
		return thresholdTransition{newActive: false, notify: true, breached: false}
	default:
		return thresholdTransition{newActive: currentlyActive}
	}
}

// normalizeThreshold turns the wire's 0-or-value sentinel into what gets
// stored: 0 clears the threshold (nil), matching how an empty
// ip_address_override clears that field elsewhere in this service. Anything
// else passes through unchanged.
func normalizeThreshold(v *int) *int {
	if v == nil || *v == 0 {
		return nil
	}
	return v
}
