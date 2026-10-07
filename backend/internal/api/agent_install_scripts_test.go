package api

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// renderInstallScript renders an installer the way ServeInstallScriptHandler
// does: its template with the server's address.
func renderInstallScript(t *testing.T, name string) string {
	t.Helper()
	tmpl, ok := installScripts[name]
	if !ok {
		t.Fatalf("no install script %q", name)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]string{"SentinelURL": "http://10.1.20.10:3001"}); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// Each installer takes ENABLE_TOOLS and TOOLS_ALLOWED_TARGETS and hands them
// to the agent: the Linux one in /etc/sentinel/agent.conf, the Docker one as
// container environment, the Windows one in the service environment.
func TestInstallScriptsPassTheToolsOptions(t *testing.T) {
	cases := []struct {
		script string
		want   []string
	}{
		{"server-agent.sh", []string{
			"ENABLE_TOOLS=true",
			`ENABLE_TOOLS="${ENABLE_TOOLS:-false}"`,
			`TOOLS_ALLOWED_TARGETS="${TOOLS_ALLOWED_TARGETS:-}"`,
			`*[!0-9./,]*) die "TOOLS_ALLOWED_TARGETS must be`,
			"\nENABLE_TOOLS=$ENABLE_TOOLS\nTOOLS_ALLOWED_TARGETS=$TOOLS_ALLOWED_TARGETS\nEOF\n",
		}},
		{"server-docker-agent.sh", []string{
			"ENABLE_TOOLS=true",
			`ENABLE_TOOLS="${ENABLE_TOOLS:-false}"`,
			`TOOLS_ALLOWED_TARGETS="${TOOLS_ALLOWED_TARGETS:-}"`,
			`*[!0-9./,]*) die "TOOLS_ALLOWED_TARGETS must be`,
			`-e ENABLE_TOOLS="$ENABLE_TOOLS" \`,
			`-e TOOLS_ALLOWED_TARGETS="$TOOLS_ALLOWED_TARGETS" \`,
			"NET_RAW is in Docker's",
		}},
		{"server-agent.ps1", []string{
			`$env:ENABLE_TOOLS="true"`,
			`if (-not $env:ENABLE_TOOLS) { $env:ENABLE_TOOLS = "false" }`,
			`"ENABLE_TOOLS=$env:ENABLE_TOOLS"`,
			`if ($env:TOOLS_ALLOWED_TARGETS) { $envLines += "TOOLS_ALLOWED_TARGETS=$env:TOOLS_ALLOWED_TARGETS" }`,
		}},
	}
	for _, tc := range cases {
		script := renderInstallScript(t, tc.script)
		for _, want := range tc.want {
			if !strings.Contains(script, want) {
				t.Errorf("%s is missing %q", tc.script, want)
			}
		}
		if !strings.Contains(script, "http://10.1.20.10:3001") {
			t.Errorf("%s lost the server address", tc.script)
		}
	}
}

// The two bash installers still parse after the edits.
func TestBashInstallScriptsParse(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	for _, name := range []string{"server-agent.sh", "server-docker-agent.sh"} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(renderInstallScript(t, name)), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(bash, "-n", path).CombinedOutput(); err != nil {
			t.Errorf("bash -n %s: %v\n%s", name, err, out)
		}
	}
}
