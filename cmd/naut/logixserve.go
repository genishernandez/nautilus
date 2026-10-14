package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/joyautomation/nautilus/lang/l5x"
	"github.com/joyautomation/nautilus/logix/deploy"
	"github.com/joyautomation/nautilus/logix/facade"
	"github.com/joyautomation/nautilus/logix/logixd"
	"github.com/joyautomation/nautilus/runtime"
	"github.com/joyautomation/nautilus/server"
)

const logixServeUsage = `naut logix serve — a Logix controller, served as a nautilus runtime

Usage:
  naut logix serve --host <controller> [flags]

Browses the controller over EtherNet/IP and serves its tags on the nautilus
runtime API, so VS Code's live values, hover, Live Values panel and
set-value — and the ladder preview's overlay on an .L5X — work against a
running Logix PLC. No Rockwell software is involved; this is the same
EtherNet/IP driver ` + "`naut run`" + ` uses.

Point the extension at it with the nautilus.runtimeUrl setting.

A write from the editor goes to the controller, and the value you then see
is what the next poll read back. Program endpoints answer honestly: there is
no nautilus source to show and nothing to warm-swap, so they name the
` + "`naut logix`" + ` verb that does the Logix version of the job.

Flags:
  --host      Controller IP or hostname (required)
  --slot      Processor backplane slot (default 0)
  --port      EtherNet/IP TCP port (default 44818)
  --tags      Comma-separated globs of controller tags to serve ("Motor*",
              "Program:MainProgram.*"). Default: every user tag except
              module I/O.
  --poll      Poll rate (default 250ms)
  --listen    API address (default :8080)
  --l5x       An L5X export of the running project: its tag descriptions
              are served on /api/meta, which a live browse cannot recover.
  --project   A nautilus project directory with a target: logix section.
              Turns on the program plane: GET /api/program serves the
              task's ladder source, and the editor's Download button
              (PUT /api/program) deploys it as an ONLINE EDIT through
              logixd ($NAUTILUS_LOGIXD_URL / _TOKEN, or target.logix.agent).
              A change that needs a download is refused with the command
              that does it. POST /api/logix/deploy is the editor's
              "Push Online Edit" and "Download Project" commands: the same
              online edit, or a DOWNLOAD (stops the controller; the body
              must say "yes": true), with progress streamed as NDJSON.
              --host defaults to target.logix.host.

Writes are open to non-browser clients and same-origin pages, like
` + "`naut run`" + `. Set NAUTILUS_TOKEN to require a bearer token — and do,
before listening anywhere but loopback: a write here changes a running PLC.
`

func runLogixServe(args []string) int {
	fs := flag.NewFlagSet("logix serve", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, logixServeUsage) }
	host := fs.String("host", "", "controller IP or hostname")
	slot := fs.Int("slot", 0, "processor backplane slot")
	port := fs.Int("port", 0, "EtherNet/IP TCP port")
	tags := fs.String("tags", "", "comma-separated tag globs")
	poll := fs.Duration("poll", 0, "poll rate")
	listen := fs.String("listen", ":8080", "API address")
	l5xPath := fs.String("l5x", "", "L5X export for tag descriptions")
	projectDir := fs.String("project", "", "nautilus project with a target: logix section")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var plane *facade.ProgramPlane
	if *projectDir != "" {
		p, err := loadLogixProject(*projectDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "naut logix serve:", err)
			return 2
		}
		if *host == "" {
			*host = p.host
		}
		if *slot == 0 {
			*slot = p.slot
		}
		if *port == 0 {
			*port = p.port
		}
		agentURL := os.Getenv("NAUTILUS_LOGIXD_URL")
		if agentURL == "" {
			agentURL = p.agent
		}
		if p.target.CommPath == "" {
			fmt.Fprintln(os.Stderr, "naut logix serve: --project needs target.logix.comm-path, the FactoryTalk Linx path logixd deploys to")
			return 2
		}
		client := logixd.New(agentURL, os.Getenv("NAUTILUS_LOGIXD_TOKEN"))
		plane = logixProgramPlane(*projectDir, p, client)
	}
	if *host == "" {
		fmt.Fprint(os.Stderr, "naut logix serve: --host is required\n\n", logixServeUsage)
		return 2
	}

	var meta map[string]runtime.TagMeta
	if *l5xPath != "" {
		var err error
		if meta, err = l5xMeta(*l5xPath); err != nil {
			fmt.Fprintln(os.Stderr, "naut logix serve:", err)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(os.Stderr, "browsing %s (slot %d)...\n", *host, *slot)
	bctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	f, err := facade.New(bctx, facade.Options{
		Host:    *host,
		Slot:    *slot,
		Port:    *port,
		Tags:    splitPatterns(*tags),
		Poll:    *poll,
		Meta:    meta,
		Server:  server.Options{AuthToken: os.Getenv("NAUTILUS_TOKEN")},
		Program: plane,
	})
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "naut logix serve:", err)
		return 1
	}
	for _, s := range f.Skipped() {
		fmt.Fprintln(os.Stderr, "  skipped", s)
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "naut logix serve:", err)
		return 1
	}
	go f.Run(ctx)
	srv := &http.Server{Handler: f.Handler()}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	fmt.Printf("nautilus · Logix %s — %d tags on http://%s — Ctrl+C to stop\n",
		*host, f.Tags(), displayAddr(ln.Addr()))
	if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "naut logix serve:", err)
		return 1
	}
	return 0
}

// l5xMeta reads tag descriptions from an export, named the way the facade
// names tags: controller scope bare, program scope "<Program>_<Tag>".
func l5xMeta(path string) (map[string]runtime.TagMeta, error) {
	f, err := l5x.ParseFile(path)
	if err != nil {
		return nil, err
	}
	tags, err := l5x.Tags(f, l5x.TagsOptions{Scope: "*", Constants: true})
	if err != nil {
		return nil, err
	}
	meta := map[string]runtime.TagMeta{}
	for _, t := range tags {
		if t.Desc != "" || t.Unit != "" {
			meta[t.Name] = runtime.TagMeta{Desc: t.Desc, Unit: t.Unit}
		}
	}
	return meta, nil
}

// displayAddr turns ":8080"'s wildcard listener into a URL a person can
// click.
func displayAddr(a net.Addr) string {
	tcp, ok := a.(*net.TCPAddr)
	if !ok || tcp.IP.IsUnspecified() {
		if ok {
			return fmt.Sprintf("localhost:%d", tcp.Port)
		}
		return a.String()
	}
	return tcp.String()
}

// logixProgramPlane is the program plane `--project` turns on: online
// edits on the warm path, downloads on the cold one, and every progress
// line both on stderr and to the editor that asked.
func logixProgramPlane(dir string, p *logixProject, client *logixd.Client) *facade.ProgramPlane {
	// One deploy runs at a time (the plane refuses a second), so one sink
	// is enough: the request's progress callback while it runs.
	var sink atomic.Pointer[func(string)]
	logf := func(format string, a ...any) {
		line := fmt.Sprintf(format, a...)
		fmt.Fprintln(os.Stderr, "deploy: "+line)
		if s := sink.Load(); s != nil {
			(*s)(line)
		}
	}
	attach := func(progress func(string)) func() {
		sink.Store(&progress)
		return func() { sink.Store(nil) }
	}
	dopts := deploy.Options{Client: client, Target: p.target, Mode: deploy.Online, Log: logf}
	// The Download button is the warm path: one project held open and
	// online, an edit is one rung import. The session opens on the first
	// edit and reopens itself if the agent drops it.
	var sess *deploy.Session
	return &facade.ProgramPlane{
		Source:     p.source,
		Controller: p.target.Controller,
		CommPath:   p.target.CommPath,
		Deploy: func(ctx context.Context, src string, progress func(string)) (*deploy.Report, error) {
			defer attach(progress)()
			if sess == nil {
				logf("opening the online session (upload, open, go online): the first edit takes about a minute")
				s, err := deploy.Connect(ctx, dopts)
				if err != nil {
					return &deploy.Report{}, err
				}
				sess = s
			}
			return sess.Edit(ctx, src)
		},
		Download: func(ctx context.Context, src string, programMode bool, progress func(string)) (*deploy.Report, error) {
			defer attach(progress)()
			// The manifest as it is now: a download is how new tags reach
			// the controller, so the target is read again, not remembered.
			fresh, err := loadLogixProject(dir)
			if err != nil {
				return &deploy.Report{}, err
			}
			if fresh.target.CommPath == "" {
				fresh.target.CommPath = p.target.CommPath
			}
			// The held session is online with the project the download
			// replaces: let go of it first.
			if sess != nil {
				logf("closing the online session: the download replaces its project")
				_ = sess.Close(ctx)
				sess = nil
			}
			o := dopts
			o.Target, o.Mode, o.ProgramMode = fresh.target, deploy.Download, programMode
			rep, err := deploy.Run(ctx, src, o)
			if err != nil {
				return rep, err
			}
			// Warm the next online edit now, so it is a rung import and
			// not another minute. Best effort: the edit opens it otherwise.
			dopts.Target = fresh.target
			logf("opening the online session for the next edit")
			if s, err := deploy.Connect(ctx, dopts); err != nil {
				logf("online session not opened (%v); the next online edit opens it", err)
			} else {
				sess = s
			}
			return rep, nil
		},
	}
}
