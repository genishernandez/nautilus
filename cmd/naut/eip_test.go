package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/joyautomation/nautilus/eip/logixserver"
)

func testEIPSpec() *logixserver.TagSurfaceSpec {
	return &logixserver.TagSurfaceSpec{
		ControllerName: "TestController",
		Templates: []logixserver.TemplateSpec{
			{Name: "Motor_Type", Members: []logixserver.MemberSpec{
				{Name: "Speed", Datatype: "REAL"},
				{Name: "Run", Datatype: "BOOL"},
			}},
		},
		Symbols: []logixserver.SymbolSpec{
			{Name: "Pump1", Datatype: "Motor_Type"},
			{Name: "Pump1Cmd", Datatype: "Motor_Type"},
		},
		Tags: []logixserver.TagSpec{
			{Path: "Pump1.Speed", Datatype: "REAL"},
			{Path: "Pump1.Run", Datatype: "BOOL"},
			{Path: "Pump1Cmd.Speed", Datatype: "REAL"},
			{Path: "Pump1Cmd.Run", Datatype: "BOOL"},
		},
	}
}

func startEIPEmulator(t *testing.T) (host string, port int) {
	t.Helper()
	schema, tags, name, err := logixserver.CompileSurface(testEIPSpec())
	if err != nil {
		t.Fatalf("compile surface: %v", err)
	}
	store := logixserver.NewTagStore()
	for _, tc := range tags {
		store.Set(tc.Path, tc.LeafType, tc.Default)
	}
	srv := logixserver.NewServer(store, schema, name, "127.0.0.1:0", nil)
	if err := srv.Listen(); err != nil {
		t.Fatalf("emulator listen: %v", err)
	}
	_, ps, _ := net.SplitHostPort(srv.Addr())
	port, _ = strconv.Atoi(ps)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx) }()
	return "127.0.0.1", port
}

func TestEIPImportCreatesOutDir(t *testing.T) {
	dir := t.TempDir()
	host, port := startEIPEmulator(t)

	// --out points at a directory tree that does not exist yet — the
	// import must create it rather than failing to write
	// eip_types.st into a missing directory.
	out := filepath.Join(dir, "generated", "eip", "central")
	args := []string{
		"import",
		"--host", host,
		"--port", strconv.Itoa(port),
		"--out", out,
		"--format", "yaml",
	}
	if code := runEIP(args); code != 0 {
		t.Fatalf("import into non-existent --out failed (%d)", code)
	}
	if _, err := os.Stat(filepath.Join(out, "eip_types.st")); err != nil {
		t.Errorf("eip_types.st not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "eip_manifest.yaml")); err != nil {
		t.Errorf("eip_manifest.yaml not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "tags", "eip.yaml")); err != nil {
		t.Errorf("tags/eip.yaml not written: %v", err)
	}
}

func TestEIPImportGoFormat(t *testing.T) {
	dir := t.TempDir()
	host, port := startEIPEmulator(t)

	// Test that go format also creates the directory
	out := filepath.Join(dir, "nested", "deep", "path")
	args := []string{
		"import",
		"--host", host,
		"--port", strconv.Itoa(port),
		"--out", out,
		"--format", "go",
		"--package", "eipdriver",
	}
	if code := runEIP(args); code != 0 {
		t.Fatalf("import into non-existent --out failed (%d)", code)
	}
	if _, err := os.Stat(filepath.Join(out, "eip_types.st")); err != nil {
		t.Errorf("eip_types.st not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "eip_manifest.go")); err != nil {
		t.Errorf("eip_manifest.go not written: %v", err)
	}

	// Read and verify the generated files contain expected content
	types, err := os.ReadFile(filepath.Join(out, "eip_types.st"))
	if err != nil {
		t.Fatalf("failed to read types file: %v", err)
	}
	if !strings.Contains(string(types), "Motor_Type") {
		t.Error("types file missing Motor_Type")
	}
}
