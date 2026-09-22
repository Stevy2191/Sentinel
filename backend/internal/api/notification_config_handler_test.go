package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestValidateChannelConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		return c
	}

	cases := []struct {
		name   string
		config models.NotificationConfig
		want   bool
	}{
		{"valid", models.NotificationConfig{Name: "Ops email", Channel: "email"}, true},
		{"empty name", models.NotificationConfig{Name: "", Channel: "email"}, false},
		{"whitespace-only name", models.NotificationConfig{Name: "   ", Channel: "email"}, false},
		{"name too long", models.NotificationConfig{Name: strings.Repeat("a", maxChannelNameLength+1), Channel: "email"}, false},
		{"name at the limit", models.NotificationConfig{Name: strings.Repeat("a", maxChannelNameLength), Channel: "email"}, true},
		{"unknown channel", models.NotificationConfig{Name: "Ops", Channel: "carrier-pigeon"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := c.config
			if got := validateChannelConfig(newCtx(), &cfg); got != c.want {
				t.Errorf("validateChannelConfig(%+v) = %v, want %v", c.config, got, c.want)
			}
		})
	}
}

func TestValidateChannelConfigTrimsName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	cfg := models.NotificationConfig{Name: "  Ops email  ", Channel: "email"}
	if !validateChannelConfig(c, &cfg) {
		t.Fatal("expected a valid config with a padded name to pass")
	}
	if cfg.Name != "Ops email" {
		t.Errorf("Name = %q, want trimmed %q", cfg.Name, "Ops email")
	}
}
