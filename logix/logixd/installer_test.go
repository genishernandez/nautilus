package logixd_test

// The installer's DOTNET_ROOT guard (tools/logixd/install.ps1). It reads
// the Machine and User scopes, which only Windows has, and it runs after
// the elevation check, so this needs an elevated Windows session — and it
// sets DOTNET_ROOT at User scope for the length of the run, so it is
// opt-in: NAUTILUS_LOGIXD_INSTALLER_TEST=1. It refuses to touch a User
// DOTNET_ROOT that is already set.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// LGX-032: the installer refuses to run while plain DOTNET_ROOT is set and
// prints the two commands that fix it, before it builds or registers
// anything.
func TestInstallerRefusesPlainDOTNET_ROOT(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("NAUTILUS_LOGIXD_INSTALLER_TEST") != "1" {
		t.Skip("set NAUTILUS_LOGIXD_INSTALLER_TEST=1 in an elevated PowerShell session on Windows; it sets DOTNET_ROOT at User scope while it runs")
	}
	pwsh := func(script string) (string, error) {
		out, err := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).CombinedOutput()
		return string(out), err
	}
	if v, _ := pwsh(`[Environment]::GetEnvironmentVariable('DOTNET_ROOT','User')`); strings.TrimSpace(v) != "" {
		t.Skipf("DOTNET_ROOT is already set at User scope (%s); not touching it", strings.TrimSpace(v))
	}
	installDir := filepath.Join(t.TempDir(), "logixd")
	script, err := filepath.Abs(filepath.Join("..", "..", "tools", "logixd", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := pwsh(`[Environment]::SetEnvironmentVariable('DOTNET_ROOT','C:\fake-x64-dotnet','User'); ` +
		`try { & '` + script + `' -TaskName logixd-installer-test -InstallDir '` + installDir + `' -WorkDir '` + t.TempDir() + `'; exit $LASTEXITCODE } ` +
		`finally { [Environment]::SetEnvironmentVariable('DOTNET_ROOT',$null,'User') }`)
	if err == nil {
		t.Fatalf("the installer ran to the end with DOTNET_ROOT set:\n%s", out)
	}
	for _, want := range []string{
		"DOTNET_ROOT is set at User scope",
		"[Environment]::SetEnvironmentVariable('DOTNET_ROOT', $null, 'User')",
		"[Environment]::SetEnvironmentVariable('DOTNET_ROOT_X64', 'C:\\fake-x64-dotnet', 'User')",
		"refusing to install",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("installer output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(installDir); err == nil {
		t.Errorf("the installer wrote %s before refusing", installDir)
	}
}
