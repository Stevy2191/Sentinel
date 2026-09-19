package models

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestIncidentComment_Validate(t *testing.T) {
	valid := func() *IncidentComment {
		return &IncidentComment{IncidentID: uuid.New(), Body: "Failed over to the secondary."}
	}

	if err := valid().Validate(); err != nil {
		t.Fatalf("a normal comment should be valid: %v", err)
	}

	// Whitespace-only is empty. Without trimming, a comment of three spaces
	// would be stored and render as a blank entry in the thread.
	for _, body := range []string{"", "   ", "\n\t "} {
		c := valid()
		c.Body = body
		if err := c.Validate(); err == nil {
			t.Errorf("body %q should be rejected", body)
		}
	}

	// A pasted log should not become a row.
	c := valid()
	c.Body = strings.Repeat("x", MaxIncidentCommentLength+1)
	if err := c.Validate(); err == nil {
		t.Error("an over-long comment should be rejected")
	}
	c.Body = strings.Repeat("x", MaxIncidentCommentLength)
	if err := c.Validate(); err != nil {
		t.Errorf("a comment at the limit should be accepted: %v", err)
	}

	c = valid()
	c.IncidentID = uuid.Nil
	if err := c.Validate(); err == nil {
		t.Error("a comment with no incident should be rejected")
	}
}
