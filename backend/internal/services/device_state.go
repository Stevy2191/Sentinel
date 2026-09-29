package services

import "github.com/Stevy2191/Sentinel/backend/internal/models"

// PollResult is what one reachability poll found.
type PollResult int

const (
	// PollOK: the device answered.
	PollOK PollResult = iota
	// PollFailed: timeout, no answer, or an SNMP error (wrong community, bad
	// v3 credentials). Counts towards down.
	PollFailed
	// PollBlocked: a configuration problem (host does not resolve, address
	// blocked by network policy, credential unusable). Not an outage.
	PollBlocked
	// PollPaused: monitoring was switched off.
	PollPaused
)

// DeviceAction is a side effect a transition asks for.
type DeviceAction string

const (
	ActionOpenIncident    DeviceAction = "open_incident"
	ActionCloseIncident   DeviceAction = "close_incident"
	ActionNotifyDown      DeviceAction = "notify_down"
	ActionNotifyRecovered DeviceAction = "notify_recovered"
)

// DeviceDownThreshold is how many consecutive failed polls mark a device
// down, the same as monitors.
const DeviceDownThreshold = 3

// DeviceTransition is a device's next state and what must happen because of it.
type DeviceTransition struct {
	Status   string
	Failures int
	Actions  []DeviceAction
}

// NextDeviceState is every up/down rule, with no I/O, so each transition is
// tested directly. Only the third consecutive failure opens an incident, and
// only recovery from down notifies; a blocked or paused device is not an
// outage, so any open incident is closed without a recovery notice. Down is
// defined as 3 *consecutive* failures, so a blocked poll — which is not a
// failure to reach the device, just a configuration problem — breaks the run
// and resets the failure counter to 0 rather than carrying it forward.
func NextDeviceState(status string, failures int, result PollResult) DeviceTransition {
	switch result {
	case PollOK:
		if status == models.DeviceStatusDown {
			return DeviceTransition{models.DeviceStatusUp, 0, []DeviceAction{ActionCloseIncident, ActionNotifyRecovered}}
		}
		return DeviceTransition{models.DeviceStatusUp, 0, nil}
	case PollFailed:
		n := failures + 1
		if status == models.DeviceStatusDown {
			return DeviceTransition{models.DeviceStatusDown, n, nil}
		}
		if n >= DeviceDownThreshold {
			return DeviceTransition{models.DeviceStatusDown, n, []DeviceAction{ActionOpenIncident, ActionNotifyDown}}
		}
		next := status
		if next == models.DeviceStatusError || next == models.DeviceStatusPaused {
			next = models.DeviceStatusPending
		}
		return DeviceTransition{next, n, nil}
	case PollBlocked:
		if status == models.DeviceStatusDown {
			return DeviceTransition{models.DeviceStatusError, 0, []DeviceAction{ActionCloseIncident}}
		}
		return DeviceTransition{models.DeviceStatusError, 0, nil}
	default: // PollPaused
		if status == models.DeviceStatusDown {
			return DeviceTransition{models.DeviceStatusPaused, 0, []DeviceAction{ActionCloseIncident}}
		}
		return DeviceTransition{models.DeviceStatusPaused, 0, nil}
	}
}
