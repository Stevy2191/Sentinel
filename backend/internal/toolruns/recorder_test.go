package toolruns

import (
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestResultLine(t *testing.T) {
	sum := func(s string) *models.RawJSON { r := models.RawJSON(s); return &r }
	text := func(s string) *string { return &s }
	cases := []struct {
		run  models.ToolRun
		want string
	}{
		{models.ToolRun{Tool: "ping", Status: "done", Summary: sum(`{"sent":5,"received":5,"loss_pct":0,"avg_ms":2.14}`)}, "0% loss, 2.1 ms avg"},
		{models.ToolRun{Tool: "ping", Status: "done", Summary: sum(`{"sent":3,"received":2,"loss_pct":33.3,"avg_ms":1.5}`)}, "33.3% loss, 1.5 ms avg"},
		{models.ToolRun{Tool: "ping", Status: "done", Summary: sum(`{"sent":5,"received":0,"loss_pct":100,"avg_ms":null}`)}, "100% loss"},
		{models.ToolRun{Tool: "traceroute", Status: "done", Summary: sum(`{"reached":true,"hop_count":9}`)}, "reached in 9 hops"},
		{models.ToolRun{Tool: "traceroute", Status: "done", Summary: sum(`{"reached":false,"hop_count":4}`)}, "not reached (4 hops)"},
		{models.ToolRun{Tool: "dns", Status: "done", Summary: sum(`{"rcode":"NOERROR","answer_count":2}`)}, "NOERROR, 2 answers"},
		{models.ToolRun{Tool: "dns", Status: "done", Summary: sum(`{"rcode":"NOERROR","answer_count":1}`)}, "NOERROR, 1 answer"},
		{models.ToolRun{Tool: "tcp", Status: "done", Summary: sum(`{"total":100,"open":3}`)}, "3 open of 100"},
		{models.ToolRun{Tool: "ping", Status: "failed", Error: text("ICMP isn't available here (needs root or NET_RAW)")}, "ICMP isn't available here (needs root or NET_RAW)"},
		{models.ToolRun{Tool: "ping", Status: "cancelled"}, "cancelled"},
		{models.ToolRun{Tool: "ping", Status: "done"}, "done"},
	}
	for _, c := range cases {
		if got := resultLine(&c.run); got != c.want {
			t.Errorf("resultLine(%s %s) = %q, want %q", c.run.Tool, c.run.Status, got, c.want)
		}
	}
}
