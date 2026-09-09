package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The derived address is what makes a fresh install work: with no base_url
// configured, commands were built with no host at all and curl refused them
// with "No host part in the URL".
func TestRequestBaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name    string
		host    string
		tls     bool
		headers map[string]string
		want    string
	}{
		{
			name: "plain request by address and port",
			host: "10.1.20.10:3001",
			want: "http://10.1.20.10:3001",
		},
		{
			name: "plain request by name",
			host: "sentinel.company.local",
			want: "http://sentinel.company.local",
		},
		{
			name: "direct TLS",
			host: "sentinel.example.com",
			tls:  true,
			want: "https://sentinel.example.com",
		},
		{
			name:    "behind a TLS-terminating proxy",
			host:    "backend:3001",
			headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "sentinel.example.com"},
			want:    "https://sentinel.example.com",
		},
		{
			// Through more than one proxy the headers arrive as a list, and the
			// entry nearest the client is the address a person actually used.
			name:    "through two proxies",
			host:    "backend:3001",
			headers: map[string]string{"X-Forwarded-Proto": "https, http", "X-Forwarded-Host": "sentinel.example.com, inner.local"},
			want:    "https://sentinel.example.com",
		},
		{
			name:    "forwarded host without a forwarded scheme",
			host:    "backend:3001",
			headers: map[string]string{"X-Forwarded-Host": "sentinel.example.com"},
			want:    "http://sentinel.example.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
			req.Host = tc.host
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req

			if got := requestBaseURL(c); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateSentinelURL(t *testing.T) {
	valid := []string{
		"",
		"http://10.1.20.10:3001",
		"https://sentinel.example.com",
		"http://sentinel.internal:8095",
		"https://sentinel.example.com/", // a trailing slash is trimmed, not rejected
	}
	for _, v := range valid {
		if err := validateSentinelURL(v); err != nil {
			t.Errorf("%q should be accepted: %v", v, err)
		}
	}

	invalid := []string{
		"sentinel.example.com",             // no scheme, so no host either
		"10.1.20.10:3001",                  // reads as scheme "10.1.20.10"
		"ftp://sentinel.example.com",       // not a scheme an agent can use
		"https://sentinel.example.com/api", // a path would be duplicated
		"javascript:alert(1)",
	}
	for _, v := range invalid {
		if err := validateSentinelURL(v); err == nil {
			t.Errorf("%q should be rejected", v)
		}
	}
}
