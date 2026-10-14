package deploy

// Against a real controller: what a download and an online edit do to the
// values the controller holds. This DOWNLOADS, which stops the controller
// and resets its tags, so it runs only when pointed at a controller that
// may be stopped — an emulator, a bench rack:
//
//	NAUTILUS_LOGIXD_URL=http://sdk-box:8188 NAUTILUS_LOGIXD_TOKEN=... \
//	NAUTILUS_LOGIX_DOWNLOAD_COMM_PATH='AB_ETH-1\10.0.0.5\Backplane\0' \
//	NAUTILUS_LOGIX_HOST=10.0.0.5 \
//	go test ./logix/deploy/ -run TestHardware -v -timeout 60m
//
// The controller is left in Program mode with DemoLine on it.

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joyautomation/nautilus/eip/logix"
	"github.com/joyautomation/nautilus/logix/logixd"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// LGA-037: a download resets tag values to the project's; an online edit
// keeps the values the controller holds.
func TestHardwareDownloadResetsValuesAndAnOnlineEditKeepsThem(t *testing.T) {
	url := os.Getenv("NAUTILUS_LOGIXD_URL")
	commPath := os.Getenv("NAUTILUS_LOGIX_DOWNLOAD_COMM_PATH")
	host := os.Getenv("NAUTILUS_LOGIX_HOST")
	if url == "" || commPath == "" || host == "" {
		t.Skip("set NAUTILUS_LOGIXD_URL, NAUTILUS_LOGIX_DOWNLOAD_COMM_PATH and NAUTILUS_LOGIX_HOST to a controller that may be downloaded to (this stops it)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	c := logixd.New(url, os.Getenv("NAUTILUS_LOGIXD_TOKEN"))
	tgt := Target{
		Controller: "DemoLine",
		Processor:  envOr("NAUTILUS_LOGIX_PROCESSOR", "1756-L85E"),
		Revision:   envOr("NAUTILUS_LOGIX_REVISION", "38.11"),
		CommPath:   commPath,
	}
	src := demoSource(t) // HiLevelSP : REAL := 85.0
	const sp = "Program:MainProgram.HiLevelSP"

	ctrl, err := logix.Dial(ctx, host)
	if err != nil {
		t.Fatalf("EtherNet/IP to %s: %v", host, err)
	}
	defer ctrl.Close()
	read := func() float32 {
		t.Helper()
		raw, err := ctrl.ReadTag(ctx, sp, 1)
		if err != nil || len(raw.Data) < 4 {
			t.Fatalf("read %s: %v (%d bytes)", sp, err, len(raw.Data))
		}
		return math.Float32frombits(binary.LittleEndian.Uint32(raw.Data))
	}
	write := func(v float64) {
		t.Helper()
		data, err := logix.EncodeScalar(0xCA, v)
		if err != nil {
			t.Fatal(err)
		}
		if err := ctrl.WriteTag(ctx, sp, 0xCA, 1, data); err != nil {
			t.Fatalf("write %s: %v", sp, err)
		}
	}
	download := func() {
		t.Helper()
		rep, err := Run(ctx, src, Options{Client: c, Target: tgt, Mode: Download, ProgramMode: true})
		if err != nil {
			t.Fatalf("download: %v", err)
		}
		if rep.Applied != Download || !rep.Verified {
			t.Fatalf("download report = %+v", rep)
		}
	}

	download()
	if got := read(); got != 85 {
		t.Fatalf("after a download HiLevelSP = %v, want the project's 85", got)
	}
	write(70)
	if got := read(); got != 70 {
		t.Fatalf("the controller did not take HiLevelSP = 70 (holds %v)", got)
	}

	// An online edit: the alarm rung changes, the tags do not.
	edited := strings.Replace(src, "GE(LevelPct, HiLevelSP)", "GT(LevelPct, HiLevelSP)", 1)
	if edited == src {
		t.Fatal("the edit changed nothing")
	}
	rep, err := Run(ctx, edited, Options{Client: c, Target: tgt, Mode: Online})
	if err != nil {
		t.Fatalf("online edit: %v", err)
	}
	if rep.Applied != Online || !rep.Verified {
		t.Fatalf("online edit report = %+v", rep)
	}
	if got := read(); got != 70 {
		t.Errorf("after an online edit HiLevelSP = %v, want the 70 the controller held", got)
	}

	// A download puts the project's value back.
	download()
	if got := read(); got != 85 {
		t.Errorf("after a download HiLevelSP = %v, want the project's 85, not the 70 the controller held", got)
	}
}
