package facade

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joyautomation/nautilus/logix/deploy"
)

// postDeploy sends POST /api/logix/deploy and returns the status and every
// NDJSON line of the answer (a plain JSON error is one line too).
func postDeploy(t *testing.T, url string, body map[string]any) (int, []map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	res, err := http.Post(url+"/api/logix/deploy", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var lines []map[string]any
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("not JSON: %q", sc.Text())
		}
		lines = append(lines, m)
	}
	return res.StatusCode, lines
}

func events(lines []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, l := range lines {
		if l["event"] == kind {
			out = append(out, l)
		}
	}
	return out
}

// The editor's Push Online Edit: progress lines as the deploy logs them,
// then the report; GET /api/program serves the new source and says it is a
// Logix target, so the extension knows which flow it is in.
func TestDeployStreamOnlineEdit(t *testing.T) {
	url := startPlaneWith(t, &ProgramPlane{
		Source: ladderA, Controller: "DemoLine", CommPath: `AB_ETH-1\10.0.0.5\Backplane\0`,
		Deploy: func(_ context.Context, src string, progress func(string)) (*deploy.Report, error) {
			progress("session open and online")
			progress("online edit live and verified")
			return &deploy.Report{Applied: deploy.Online, Replaced: 2, Verified: true, ModeBefore: "Run", ModeAfter: "Run"}, nil
		},
	})
	info := getProgram(t, url)
	if info["target"] != "logix" || info["controller"] != "DemoLine" || info["canDownload"] != false || !strings.Contains(info["commPath"].(string), "10.0.0.5") {
		t.Fatalf("info = %v", info)
	}
	code, lines := postDeploy(t, url, map[string]any{"mode": "online", "source": ladderB, "baseHash": info["hash"]})
	if code != 200 {
		t.Fatalf("%d %v", code, lines)
	}
	prog := events(lines, "progress")
	if len(prog) != 2 || prog[0]["message"] != "session open and online" {
		t.Errorf("progress = %v", prog)
	}
	done := events(lines, "done")
	if len(done) != 1 || done[0]["applied"] != "online edit" || done[0]["verified"] != true || done[0]["mode"] != "Run" {
		t.Fatalf("done = %v (all: %v)", done, lines)
	}
	if lines[len(lines)-1]["event"] != "done" {
		t.Error("the report is not the last line")
	}
	if after := getProgram(t, url); after["source"] != ladderB || after["hash"] != done[0]["hash"] {
		t.Errorf("after = %v", after)
	}
}

// A download stops the controller: refused without "yes", refused where
// the server has no download, and otherwise run with the Program-mode
// choice the editor sent.
func TestDeployStreamDownload(t *testing.T) {
	var calls int
	var gotMode bool
	plane := &ProgramPlane{
		Source: ladderA, CommPath: `AB_ETH-1\10.0.0.5\Backplane\0`,
		Deploy: func(context.Context, string, func(string)) (*deploy.Report, error) {
			t.Error("a download ran the online-edit path")
			return &deploy.Report{}, nil
		},
		Download: func(_ context.Context, src string, programMode bool, progress func(string)) (*deploy.Report, error) {
			calls++
			gotMode = programMode
			progress("imported and built")
			return &deploy.Report{Applied: deploy.Download, Verified: true, ModeBefore: "Run", ModeAfter: "Program"}, nil
		},
	}
	url := startPlaneWith(t, plane)
	if getProgram(t, url)["canDownload"] != true {
		t.Fatal("canDownload is false with a Download configured")
	}

	code, lines := postDeploy(t, url, map[string]any{"mode": "download", "source": ladderB})
	if code != 400 || !strings.Contains(lines[0]["error"].(string), "STOPS the controller") || calls != 0 {
		t.Fatalf("no yes: %d %v (calls %d)", code, lines, calls)
	}

	code, lines = postDeploy(t, url, map[string]any{"mode": "download", "source": ladderB, "yes": true, "programMode": true})
	if code != 200 || calls != 1 || !gotMode {
		t.Fatalf("download: %d %v (calls %d, programMode %v)", code, lines, calls, gotMode)
	}
	if p := events(lines, "progress"); len(p) < 2 || !strings.Contains(p[0]["message"].(string), "download to AB_ETH-1\\10.0.0.5") {
		t.Errorf("progress = %v", p)
	}
	done := events(lines, "done")
	if len(done) != 1 || done[0]["applied"] != "download" || done[0]["mode"] != "Program" || done[0]["modeBefore"] != "Run" {
		t.Fatalf("done = %v", lines)
	}
	if getProgram(t, url)["source"] != ladderB {
		t.Error("a download did not become the served source")
	}

	noDL := startPlaneWith(t, &ProgramPlane{Source: ladderA, Deploy: plane.Deploy})
	if code, lines := postDeploy(t, noDL, map[string]any{"mode": "download", "source": ladderB, "yes": true}); code != 501 {
		t.Errorf("no Download configured: %d %v", code, lines)
	}
}

// A refusal from the controller side arrives as the stream's error event,
// with the status the PUT would have answered; nothing changes.
func TestDeployStreamRefusal(t *testing.T) {
	url := startPlaneWith(t, &ProgramPlane{Source: ladderA,
		Deploy: func(_ context.Context, _ string, progress func(string)) (*deploy.Report, error) {
			progress("comparing with the controller")
			return &deploy.Report{}, &deploy.NeedsDownloadError{Diffs: []string{"tag X: only on the second side"}}
		}})
	code, lines := postDeploy(t, url, map[string]any{"mode": "online", "source": ladderB})
	if code != 200 {
		t.Fatalf("%d %v", code, lines)
	}
	errs := events(lines, "error")
	if len(errs) != 1 || errs[0]["status"] != float64(422) || !strings.Contains(errs[0]["error"].(string), "Download Project to Logix Controller") {
		t.Fatalf("lines = %v", lines)
	}
	if getProgram(t, url)["source"] != ladderA {
		t.Error("a refused edit changed the served source")
	}
}

// What needs no controller is answered before the stream: a bad mode, a
// stale base, a second deploy while one runs, and a serve with no project.
func TestDeployStreamUpfrontRefusals(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	url := startPlaneWith(t, &ProgramPlane{Source: ladderA,
		Deploy: func(context.Context, string, func(string)) (*deploy.Report, error) {
			started <- struct{}{}
			<-release
			return &deploy.Report{Applied: deploy.Online}, nil
		}})
	if code, _ := postDeploy(t, url, map[string]any{"mode": "build", "source": ladderB}); code != 400 {
		t.Errorf("bad mode: %d", code)
	}
	if code, lines := postDeploy(t, url, map[string]any{"mode": "online", "source": ladderB, "baseHash": "deadbeef0000"}); code != 409 || !strings.Contains(lines[0]["error"].(string), "changed since your base") {
		t.Errorf("stale base: %d %v", code, lines)
	}
	first := make(chan int, 1)
	go func() {
		raw, _ := json.Marshal(map[string]any{"mode": "online", "source": ladderB})
		res, err := http.Post(url+"/api/logix/deploy", "application/json", bytes.NewReader(raw))
		if err != nil {
			first <- 0
			return
		}
		_, _ = bufio.NewReader(res.Body).ReadString(0)
		res.Body.Close()
		first <- res.StatusCode
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first deploy never started")
	}
	if code, lines := postDeploy(t, url, map[string]any{"mode": "online", "source": ladderB}); code != 409 || !strings.Contains(lines[0]["error"].(string), "already in progress") {
		t.Errorf("second deploy: %d %v", code, lines)
	}
	close(release)
	if code := <-first; code != 200 {
		t.Errorf("the first deploy: %d", code)
	}

	bare := startPlaneWith(t, nil)
	if code, lines := postDeploy(t, bare, map[string]any{"mode": "online", "source": ladderB}); code != 404 || !strings.Contains(lines[0]["error"].(string), "--project") {
		t.Errorf("no project: %d %v", code, lines)
	}
}
