package services

import (
	"reflect"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestNextDeviceState(t *testing.T) {
	type A = []DeviceAction
	cases := []struct {
		name     string
		status   string
		failures int
		result   PollResult
		want     DeviceTransition
	}{
		{"first success", models.DeviceStatusPending, 0, PollOK, DeviceTransition{models.DeviceStatusUp, 0, nil}},
		{"success stays up", models.DeviceStatusUp, 0, PollOK, DeviceTransition{models.DeviceStatusUp, 0, nil}},
		{"success after one failure", models.DeviceStatusUp, 1, PollOK, DeviceTransition{models.DeviceStatusUp, 0, nil}},
		{"one failure", models.DeviceStatusUp, 0, PollFailed, DeviceTransition{models.DeviceStatusUp, 1, nil}},
		{"two failures", models.DeviceStatusUp, 1, PollFailed, DeviceTransition{models.DeviceStatusUp, 2, nil}},
		{"third failure goes down", models.DeviceStatusUp, 2, PollFailed,
			DeviceTransition{models.DeviceStatusDown, 3, A{ActionOpenIncident, ActionNotifyDown}}},
		{"pending device never answering goes down", models.DeviceStatusPending, 2, PollFailed,
			DeviceTransition{models.DeviceStatusDown, 3, A{ActionOpenIncident, ActionNotifyDown}}},
		{"further failures while down do nothing", models.DeviceStatusDown, 3, PollFailed,
			DeviceTransition{models.DeviceStatusDown, 4, nil}},
		{"recovery", models.DeviceStatusDown, 7, PollOK,
			DeviceTransition{models.DeviceStatusUp, 0, A{ActionCloseIncident, ActionNotifyRecovered}}},
		{"blocked is a configuration error", models.DeviceStatusUp, 1, PollBlocked, DeviceTransition{models.DeviceStatusError, 0, nil}},
		{"blocked while down closes quietly", models.DeviceStatusDown, 3, PollBlocked,
			DeviceTransition{models.DeviceStatusError, 0, A{ActionCloseIncident}}},
		{"failing after an error starts counting as pending", models.DeviceStatusError, 0, PollFailed,
			DeviceTransition{models.DeviceStatusPending, 1, nil}},
		{"pause", models.DeviceStatusUp, 1, PollPaused, DeviceTransition{models.DeviceStatusPaused, 0, nil}},
		{"pause while down closes the incident", models.DeviceStatusDown, 4, PollPaused,
			DeviceTransition{models.DeviceStatusPaused, 0, A{ActionCloseIncident}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextDeviceState(tc.status, tc.failures, tc.result)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
