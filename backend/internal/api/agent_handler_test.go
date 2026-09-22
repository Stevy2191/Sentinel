package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestValidateThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		return c
	}
	v := func(n int) *int { return &n }

	cases := []struct {
		name string
		in   *int
		want bool
	}{
		{"nil is valid (not sent)", nil, true},
		{"0 is valid (the clear sentinel)", v(0), true},
		{"1 is valid", v(1), true},
		{"100 is valid", v(100), true},
		{"101 is invalid", v(101), false},
		{"negative is invalid", v(-1), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validateThreshold(newCtx(), "cpu_threshold_percent", c.in); got != c.want {
				t.Errorf("validateThreshold(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
