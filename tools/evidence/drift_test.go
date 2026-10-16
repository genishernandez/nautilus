package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDocsDrift(t *testing.T) {
	files := []ClaimFile{
		{Page: "website/src/content/docs/guides/modbus.md", File: "modbus.yaml"},
		{Page: "website/src/content/docs/guides/sparkplug.md", File: "sparkplug.yaml"},
		{Page: "website/src/content/docs/guides/alarms.md", File: "alarms.yaml"},
	}
	changed := []string{
		"website/src/content/docs/guides/modbus.md",    // page only: drift
		"website/src/content/docs/guides/sparkplug.md", // page and claims: fine
		"docs/claims/sparkplug.yaml",
		"docs/claims/alarms.yaml", // claims only: fine
		"sparkplug/node.go",
	}
	got := docsDrift(files, changed)
	if len(got) != 1 || got[0].File != "modbus.yaml" {
		t.Fatalf("drift = %+v, want modbus.yaml only", got)
	}
}

// The real claims files: a changed page with an unchanged claims file is
// reported, as an annotation with -github.
func TestRunDriftAnnotates(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("website/src/content/docs/guides/modbus.md\nREADME.md\n")
	if err := runDrift([]string{"-root", "../..", "-github"}, in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "::warning file=website/src/content/docs/guides/modbus.md") ||
		strings.Count(out.String(), "::warning") != 1 {
		t.Errorf("output:\n%s", out.String())
	}
}
