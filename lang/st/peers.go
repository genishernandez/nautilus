package st

import (
	"fmt"
	"strings"

	"github.com/joyautomation/nautilus/lang/ir"
)

// PeerProgram is another program of the same project, as far as a
// diagnostic needs it: its name, the task that runs it, and the variables
// it declares for itself. It never puts anything in scope. Tasks share
// only tags (#327): a program's VARs, its function-block instances among
// them, live in that program's private frame, so `t1.Q` or `A.t1.Q` from
// another program is an undeclared identifier. The peers only let that
// error say why, and what to do instead.
type PeerProgram struct {
	Program string   // the PROGRAM name
	Task    string   // the task that runs it; "" when not known
	Locals  []string // its program-level variables, as declared
}

// PeerOf describes a parsed program as a peer: its PROGRAM name and every
// variable it declares at program level — VAR (instances included),
// VAR_TEMP, VAR_INPUT/OUTPUT/IN_OUT — but not the tags it names
// (VAR_EXTERNAL, VAR_GLOBAL), which every program can already reach. ok is
// false for a source with no PROGRAM (a library).
func PeerOf(prog *Program) (PeerProgram, bool) {
	if prog == nil || prog.Name == "" || prog.TopKeyword != "PROGRAM" {
		return PeerProgram{}, false
	}
	p := PeerProgram{Program: prog.Name}
	for _, b := range prog.VarBlocks {
		if b.FileScope || b.Kind == "VAR_EXTERNAL" || b.Kind == "VAR_GLOBAL" {
			continue
		}
		for _, v := range b.Variables {
			p.Locals = append(p.Locals, v.Name)
		}
	}
	return p, true
}

// peerHint explains an undeclared identifier that names another program,
// its task, or one of its variables; "" when it names none of them. The
// program being lowered is skipped, so passing every program of a project
// as the peers of each is fine.
func (l *lowerer) peerHint(name string) string {
	for _, p := range l.peers {
		if strings.EqualFold(p.Program, l.prog.Name) {
			continue
		}
		var what string
		switch {
		case strings.EqualFold(name, p.Program):
			what = name + " is a program"
			if p.Task != "" {
				what += " (task " + p.Task + ")"
			}
			what += ", and a program's variables cannot be addressed through its name"
		case p.Task != "" && strings.EqualFold(name, p.Task):
			what = fmt.Sprintf("%s is the task running program %s, and a program's variables cannot be addressed through its task", name, p.Program)
		default:
			for _, v := range p.Locals {
				if ir.NameKey(v) == ir.NameKey(name) {
					what = fmt.Sprintf("%s is a local of program %s", name, p.Program)
					if p.Task != "" {
						what += " (task " + p.Task + ")"
					}
					what += ", and one program cannot read another's variables"
					break
				}
			}
		}
		if what != "" {
			return what + fmt.Sprintf(". Tasks share only tags: in %s, write the value to a tag (an assignment or a coil) and read that tag here", p.Program)
		}
	}
	return ""
}
