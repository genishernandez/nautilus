package main

// The program plane `naut logix serve --project` builds (logixProgramPlane):
// what the editor's Download Project and Push Online Edit commands reach
// through POST /api/logix/deploy, driven here against the fake agent. The
// facade tests prove the endpoint; these prove the session handling behind
// it — a download lets go of the held online session first (it is online
// with the project the download replaces) and opens a fresh one after, so
// the next online edit is warm — and that every deploy line reaches the
// editor's progress.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/logix/deploy"
	"github.com/joyautomation/nautilus/logix/logixd"
)

// emptyLine is a controller named Line with no program in it yet.
const emptyLine = `<?xml version="1.0"?><RSLogix5000Content><Controller Name="Line"><Programs/></Controller></RSLogix5000Content>`

func TestLogixServePlaneDownloadRewarmsTheSession(t *testing.T) {
	t.Setenv("NAUTILUS_LOGIXD_URL", "")
	f, url, stop := newFakeLogixd(t)
	defer stop()
	f.controllerL5X = emptyLine
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nautilus.yaml"), []byte(fillManifest(url, "    comm-path: AB_ETH-1\\10.0.0.5\\Backplane\\0\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Fill.ld"), []byte(fillLadder), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := loadLogixProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	progress := func(s string) { lines = append(lines, s) }
	stderr, _ := captureStderr(t, func() int {
		plane := logixProgramPlane(dir, p, logixd.New(url, ""))
		if plane.Controller != "Line" || !strings.Contains(plane.CommPath, "10.0.0.5") || plane.Download == nil {
			t.Fatalf("plane = %+v", plane)
		}
		ctx := context.Background()

		// The first online edit opens the session. The fake controller has
		// no Fill routine, so the edit itself is refused: a download is due.
		if _, err := plane.Deploy(ctx, fillLadder, progress); err == nil {
			t.Fatal("an edit to a controller without the routine was accepted")
		} else if _, ok := err.(*deploy.NeedsDownloadError); !ok {
			t.Fatalf("first edit: %v", err)
		}
		if !hasLine(lines, "opening the online session") {
			t.Errorf("progress = %q", lines)
		}

		// The download's verification compares the controller's upload with
		// what was sent: let the fake controller hold what the deploy
		// generates, as a real one does after the download.
		_, _ = plane.Download(ctx, fillLadder, true, func(string) {})
		gen := ""
		for name, raw := range f.files {
			if strings.HasSuffix(name, "/Line.L5X") {
				gen = string(raw)
			}
		}
		if gen == "" {
			t.Fatalf("no project reached the agent: %v", f.seen)
		}
		f.controllerL5X = gen

		f.seen, lines = nil, nil
		rep, err := plane.Download(ctx, fillLadder, true, progress)
		if err != nil || rep.Applied != deploy.Download || !rep.Verified {
			t.Fatalf("download: %v %+v", err, rep)
		}
		dl, reopen := indexOf(f.seen, "/download"), lastIndexOf(f.seen, "/upload-to-new")
		if dl < 0 || reopen < dl {
			t.Errorf("no session reopened after the download: %v", f.seen)
		}
		if !hasLine(lines, "opening the online session for the next edit") || !hasLine(lines, "session open and online") {
			t.Errorf("download progress = %q", lines)
		}

		// The next edit is warm: no upload, just the rung import.
		f.seen = nil
		edited := strings.Replace(fillLadder, "RUNG step Start", "RUNG step /Start", 1)
		_, _ = plane.Deploy(ctx, edited, progress)
		if indexOf(f.seen, "/upload-to-new") >= 0 || indexOf(f.seen, "/import-rungs") < 0 {
			t.Errorf("the edit after a download was not warm: %v", f.seen)
		}
		return 0
	})
	if !strings.Contains(stderr, "deploy: ") {
		t.Errorf("deploy lines did not reach stderr: %q", stderr)
	}
}

// A session held open by an online edit is closed before a download, and
// the download takes nautilus.yaml as it is now.
func TestLogixServePlaneReleasesTheSessionBeforeADownload(t *testing.T) {
	t.Setenv("NAUTILUS_LOGIXD_URL", "")
	f, url, stop := newFakeLogixd(t)
	defer stop()
	f.controllerL5X = emptyLine
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "nautilus.yaml"), []byte(fillManifest(url, "    comm-path: AB_ETH-1\\10.0.0.5\\Backplane\\0\n")), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "Fill.ld"), []byte(fillLadder), 0o644)
	p, err := loadLogixProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	captureStderr(t, func() int {
		plane := logixProgramPlane(dir, p, logixd.New(url, ""))
		_, _ = plane.Deploy(context.Background(), fillLadder, func(string) {}) // opens a session
		// The manifest changes after the server started: the download
		// takes it as it is now.
		edited := strings.Replace(fillManifest(url, "    comm-path: AB_ETH-1\\10.0.0.5\\Backplane\\0\n"),
			"{ name: Speed, role: setpoint, type: INT, init: 0 }",
			`{ name: Speed, role: setpoint, type: INT, init: 0, desc: "conveyor speed setpoint" }`, 1)
		if err := os.WriteFile(filepath.Join(dir, "nautilus.yaml"), []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
		f.seen = nil
		_, _ = plane.Download(context.Background(), fillLadder, false, func(string) {})
		closed := indexOf(f.seen, "DELETE /v1/sessions/")
		if closed < 0 || closed > indexOf(f.seen, "/download") {
			t.Errorf("the held session was not closed before the download: %v", f.seen)
		}
		sent := ""
		for name, raw := range f.files {
			if strings.HasSuffix(name, "/Line.L5X") && strings.HasPrefix(name, "deploy-") {
				sent += string(raw)
			}
		}
		if !strings.Contains(sent, "conveyor speed setpoint") {
			t.Error("the download did not take nautilus.yaml as it is now")
		}
		return 0
	})
}

func hasLine(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func indexOf(seen []string, sub string) int {
	for i, s := range seen {
		if strings.Contains(s, sub) {
			return i
		}
	}
	return -1
}

func lastIndexOf(seen []string, sub string) int {
	for i := len(seen) - 1; i >= 0; i-- {
		if strings.Contains(seen[i], sub) {
			return i
		}
	}
	return -1
}
