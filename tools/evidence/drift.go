package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// runDrift reads changed paths (one per line, as `git diff --name-only`
// prints them) and reports each documentation page that changed while its
// claims file did not: the page may now promise something its claims don't
// list, or no longer promise something they do. A warning, never a failure;
// CI prints it as an annotation and in the PR's job summary.
func runDrift(args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("drift", flag.ExitOnError)
	root := fs.String("root", ".", "repository root")
	gha := fs.Bool("github", false, "print GitHub Actions ::warning annotations")
	summary := fs.String("summary", "", "append the report as markdown to this file")
	fs.Parse(args)
	files, _, err := loadClaims(filepath.Join(*root, "docs", "claims"))
	if err != nil {
		return err
	}
	var changed []string
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		if p := strings.TrimSpace(sc.Text()); p != "" {
			changed = append(changed, p)
		}
	}
	drifted := docsDrift(files, changed)
	for _, d := range drifted {
		if *gha {
			fmt.Fprintf(out, "::warning file=%s,title=claims not updated::%s changed but docs/claims/%s did not — check its claims still say what the page says\n", d.Page, d.Page, d.File)
		} else {
			fmt.Fprintf(out, "%s changed, docs/claims/%s did not\n", d.Page, d.File)
		}
	}
	if *summary != "" && len(drifted) > 0 {
		var b strings.Builder
		b.WriteString("\n### Docs changed, claims didn't\n\nCheck each page's claims still say what the page says (docs/claims/README.md):\n\n")
		for _, d := range drifted {
			fmt.Fprintf(&b, "- `%s` → `docs/claims/%s`\n", d.Page, d.File)
		}
		f, err := os.OpenFile(*summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(b.String()); err != nil {
			return err
		}
	}
	return nil
}

// docsDrift returns the claim files whose page is among changed and which
// are not themselves among changed, sorted by page.
func docsDrift(files []ClaimFile, changed []string) []ClaimFile {
	set := map[string]bool{}
	for _, p := range changed {
		set[filepath.ToSlash(p)] = true
	}
	var out []ClaimFile
	for _, f := range files {
		if set[f.Page] && !set["docs/claims/"+f.File] {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Page < out[j].Page })
	return out
}
