package logixd_test

// The agent's startup and request policy, tested by starting a logixd
// process of our own with the configuration under test. These need the
// built agent (LOGIXD_BIN, the directory `dotnet build -o` wrote, and
// LOGIXD_DOTNET, the dotnet that runs it), which exists only on the
// licensed Windows machine: the logix workflow's self-hosted job sets both
// and runs TestAgent*. Everywhere else they skip.
//
// None of this needs an SDK licence or a controller: the token check is
// at startup, and the allowlist is checked before the SDK is called.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joyautomation/nautilus/logix/logixd"
)

// logixdProcess builds the command that runs the built agent with env
// layered over this process's environment.
func logixdProcess(t *testing.T, env map[string]string) *exec.Cmd {
	t.Helper()
	bin := os.Getenv("LOGIXD_BIN")
	if bin == "" {
		t.Skip("set LOGIXD_BIN (and LOGIXD_DOTNET) to a built logixd to test its startup and request policy; it builds only beside the Logix Designer SDK")
	}
	dotnet := os.Getenv("LOGIXD_DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	cmd := exec.Command(dotnet, filepath.Join(bin, "logixd.dll"))
	base := map[string]string{"LOGIXD_WORKDIR": t.TempDir()}
	if dotnet != "dotnet" {
		// DOTNET_ROOT_X64, never plain DOTNET_ROOT (docs/design/logix-target.md §19.5).
		base["DOTNET_ROOT_X64"] = filepath.Dir(dotnet)
	}
	for k, v := range env {
		base[k] = v
	}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, override := base[k]; override || strings.EqualFold(k, "DOTNET_ROOT") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	for k, v := range base {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// startLogixd runs an agent and waits for its health endpoint.
func startLogixd(t *testing.T, env map[string]string) *logixd.Client {
	t.Helper()
	port := freePort(t)
	env["LOGIXD_ADDR"] = fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd := logixdProcess(t, env)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting logixd: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	c := logixd.New(env["LOGIXD_ADDR"], env["LOGIXD_TOKEN"])
	deadline := time.Now().Add(60 * time.Second)
	for {
		cx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := c.Health(cx)
		cancel()
		if err == nil {
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("logixd never answered: %v\n%s", err, stderr.String())
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// LGX-028: on a routable address the agent requires a token, and refuses
// to start without one.
func TestAgentRefusesARoutableBindWithoutAToken(t *testing.T) {
	addr := fmt.Sprintf("http://0.0.0.0:%d", freePort(t))
	cmd := logixdProcess(t, map[string]string{"LOGIXD_ADDR": addr, "LOGIXD_TOKEN": ""})
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting logixd: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 {
			t.Errorf("logixd on %s without a token exited with %v, want exit status 2", addr, err)
		}
		if !strings.Contains(stderr.String(), "refusing to bind") {
			t.Errorf("stderr does not say why:\n%s", stderr.String())
		}
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("logixd started on %s with no token; it must refuse", addr)
	}

	// With a token the same bind starts, so the refusal is the token's.
	port := freePort(t)
	ok := logixdProcess(t, map[string]string{"LOGIXD_ADDR": fmt.Sprintf("http://0.0.0.0:%d", port), "LOGIXD_TOKEN": "routable-test"})
	if err := ok.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ok.Process.Kill(); _ = ok.Wait() })
	c := logixd.New(fmt.Sprintf("http://127.0.0.1:%d", port), "routable-test")
	deadline := time.Now().Add(60 * time.Second)
	for {
		cx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := c.Health(cx)
		cancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("logixd with a token did not start on a routable bind: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// LGX-029: LOGIXD_COMM_ALLOW is the agent's configuration; a request that
// names a controller outside it is refused before the SDK is asked.
func TestAgentRefusesACommPathOutsideItsAllowlist(t *testing.T) {
	const allowed = `AB_ETH-1\192.0.2.10\Backplane\0` // TEST-NET-1: never a real controller
	c := startLogixd(t, map[string]string{"LOGIXD_COMM_ALLOW": allowed, "LOGIXD_TOKEN": ""})
	cx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	h, err := c.Health(cx)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.CommAllowlist) != 1 || h.CommAllowlist[0] != allowed {
		t.Fatalf("health reports allowlist %v, want [%s]", h.CommAllowlist, allowed)
	}
	_, err = c.UploadToNew(cx, `AB_ETH-1\192.0.2.11\Backplane\0`, "it-allowlist/controller.ACD")
	var e *logixd.Error
	if !errors.As(err, &e) || e.Status != http.StatusForbidden || e.Kind != "forbidden" {
		t.Fatalf("a comm path outside the allowlist: want 403 forbidden, got %v", err)
	}
	if !strings.Contains(e.Message, "allowlist") {
		t.Errorf("the refusal does not name the allowlist: %s", e.Message)
	}
}

// LGX-031: an agent with a token answers a caller with no token, and one
// with the wrong token, with 401.
func TestAgentAnswersAMissingOrWrongTokenWith401(t *testing.T) {
	t.Setenv("NAUTILUS_LOGIXD_TOKEN", "") // so a client built with no token sends none
	c := startLogixd(t, map[string]string{"LOGIXD_TOKEN": "right-token"})
	for _, token := range []string{"", "wrong-token"} {
		cx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := logixd.New(c.BaseURL, token).Health(cx)
		cancel()
		var e *logixd.Error
		if !errors.As(err, &e) || e.Status != http.StatusUnauthorized {
			t.Errorf("token %q: want 401, got %v", token, err)
		}
	}
}
