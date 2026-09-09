package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/gin-gonic/gin"
)

// agentDistDir holds the cross-built agent binaries, placed there by the
// Dockerfile. Overridable so the server can run outside a container.
func agentDistDir() string {
	if dir := strings.TrimSpace(os.Getenv("AGENT_DIST_DIR")); dir != "" {
		return dir
	}
	return "./agent-dist"
}

// supportedAgentArch maps what `uname -m` prints to the names the binaries are
// built under, so an installer can pass the machine's own answer straight
// through.
var supportedAgentArch = map[string]string{
	"amd64": "amd64", "x86_64": "amd64",
	"arm64": "arm64", "aarch64": "arm64",
}

// DownloadAgentBinaryHandler serves the agent binary for an architecture.
//
// Unauthenticated on purpose. The binary is not a secret — it is the same
// build for everyone, and it does nothing without an agent id and token. A
// host being provisioned generally has the token but no user session, and
// requiring one would mean putting a person's credentials into an install
// script.
func DownloadAgentBinaryHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		arch, ok := supportedAgentArch[strings.ToLower(c.Param("arch"))]
		if !ok {
			respondError(c, http.StatusNotFound,
				"no agent build for that architecture; amd64 and arm64 are available")
			return
		}
		// The name is built from a value already matched against the map
		// above, so nothing from the request reaches the path.
		path := filepath.Join(agentDistDir(), "sentinel-agent-linux-"+arch)
		if _, err := os.Stat(path); err != nil {
			respondError(c, http.StatusNotFound,
				"the agent binary is not bundled with this server build")
			return
		}
		c.Header("Content-Disposition", `attachment; filename="sentinel-agent"`)
		c.File(path)
	}
}

// installScriptTemplates holds the two installers. They are templates rather
// than static files so the server's own URL is baked in, which is the one
// value an operator would otherwise have to fill in by hand and the one most
// likely to be got wrong.
var installScripts = map[string]*template.Template{
	"server-agent.sh":        template.Must(template.New("bash").Parse(bashInstallScript)),
	"server-docker-agent.sh": template.Must(template.New("docker").Parse(dockerInstallScript)),
}

// ServeInstallScriptHandler serves an installation script.
func ServeInstallScriptHandler(baseURL func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tmpl, ok := installScripts[c.Param("script")]
		if !ok {
			respondError(c, http.StatusNotFound, "no such install script")
			return
		}
		url := strings.TrimRight(strings.TrimSpace(baseURL()), "/")
		if url == "" {
			// Falls back to the requesting host so the script is still usable
			// on an install where base_url has not been set yet.
			scheme := "http"
			if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
				scheme = "https"
			}
			url = fmt.Sprintf("%s://%s", scheme, c.Request.Host)
		}

		c.Header("Content-Type", "text/x-shellscript; charset=utf-8")
		if err := tmpl.Execute(c.Writer, map[string]string{"SentinelURL": url}); err != nil {
			// The status is already written by this point, so there is nothing
			// to do but record it.
			fmt.Fprintf(os.Stderr, "[agent] rendering install script: %v\n", err)
		}
	}
}

// RegisterAgentInstallRoutes mounts the unauthenticated installer endpoints on
// the router, outside the API group that requires a user session.
func RegisterAgentInstallRoutes(router *gin.Engine, baseURL func() string) {
	router.GET("/agent/download/linux/:arch", DownloadAgentBinaryHandler())
	router.GET("/scripts/:script", ServeInstallScriptHandler(baseURL))
}
