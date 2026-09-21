package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
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

// supportedWindowsAgentArch is separate from supportedAgentArch: the
// Dockerfile only cross-builds Windows for amd64, so arm64 must 404 there
// even though it is a real Linux build target.
var supportedWindowsAgentArch = map[string]string{
	"amd64": "amd64", "x86_64": "amd64",
}

// DownloadWindowsAgentBinaryHandler serves the Windows agent binary. Kept as
// its own route and handler, rather than folding "linux"/"windows" into one
// parameterized route, so the existing Linux download path — already in use
// by every install script — is untouched.
func DownloadWindowsAgentBinaryHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		arch, ok := supportedWindowsAgentArch[strings.ToLower(c.Param("arch"))]
		if !ok {
			respondError(c, http.StatusNotFound,
				"no agent build for that architecture; amd64 is available")
			return
		}
		path := filepath.Join(agentDistDir(), "sentinel-agent-windows-"+arch+".exe")
		if _, err := os.Stat(path); err != nil {
			respondError(c, http.StatusNotFound,
				"the agent binary is not bundled with this server build")
			return
		}
		c.Header("Content-Disposition", `attachment; filename="sentinel-agent.exe"`)
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
	"server-agent.ps1":       template.Must(template.New("powershell").Parse(windowsInstallScript)),
}

// ServeInstallScriptHandler serves an installation script.
func ServeInstallScriptHandler(settings *services.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		tmpl, ok := installScripts[c.Param("script")]
		if !ok {
			respondError(c, http.StatusNotFound, "no such install script")
			return
		}
		// The internal address, because this value becomes the agent's
		// SENTINEL_URL — where it reports back — not where the script was
		// downloaded from. Behind a proxy those differ.
		url := resolveSentinelURLs(c, settings).Internal

		// Checked again here even though the sources are validated, because
		// what is being written is a shell script that will be run with sudo.
		// A value that cannot be vouched for produces an error an operator can
		// act on rather than a script that might carry something else.
		if err := validateSentinelURL(url); err != nil || url == "" {
			respondError(c, http.StatusServiceUnavailable,
				"this server cannot determine its own address; set the external and internal URLs under Settings -> System, then download the script again")
			return
		}

		contentType := "text/x-shellscript; charset=utf-8"
		if strings.HasSuffix(c.Param("script"), ".ps1") {
			contentType = "text/plain; charset=utf-8"
		}
		c.Header("Content-Type", contentType)
		if err := tmpl.Execute(c.Writer, map[string]string{"SentinelURL": url}); err != nil {
			// The status is already written by this point, so there is nothing
			// to do but record it.
			fmt.Fprintf(os.Stderr, "[agent] rendering install script: %v\n", err)
		}
	}
}

// RegisterAgentInstallRoutes mounts the unauthenticated installer endpoints on
// the router, outside the API group that requires a user session.
func RegisterAgentInstallRoutes(router *gin.Engine, settings *services.SettingsService) {
	router.GET("/agent/download/linux/:arch", DownloadAgentBinaryHandler())
	router.GET("/agent/download/windows/:arch", DownloadWindowsAgentBinaryHandler())
	router.GET("/scripts/:script", ServeInstallScriptHandler(settings))
}
