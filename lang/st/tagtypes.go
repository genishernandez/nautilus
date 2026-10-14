package st

import (
	"errors"
	"fmt"
	"strings"

	"github.com/joyautomation/nautilus/lang/ir"
)

// Hooks for project composition: what a manifest tag's `type:` means, and
// which globals a GVL file declares. They sit beside the lowerer, not in
// it, and reuse its type resolution so a tag's type and a VAR_EXTERNAL's
// can never be read two different ways.

// Types resolves the TYPE declarations of a parsed source — its own TYPE
// blocks, which in a composed source include every library's — without
// lowering any POU. The result keys each TYPE by its declared spelling.
//
// Each FUNCTION_BLOCK the source declares is in the result too, as a
// TypeFB with no FBDef, after the TYPEs resolve (so no TYPE can reach
// one): enough for ResolveTypeName to refuse a project block by name, as
// it refuses a standard one. Callers that want only the TYPEs skip
// Kind == ir.TypeFB.
func Types(prog *Program) (map[string]*ir.Type, error) {
	l := newLowerer(prog, nil)
	// The same sequence LowerWithOpts runs: shells and enumerations, the
	// project constants (an array bound may name one), then the rest.
	if err := l.collectTypeShells(); err != nil {
		return nil, err
	}
	if err := l.collectGlobalConsts(); err != nil {
		return nil, err
	}
	if err := l.collectTypes(); err != nil {
		return nil, err
	}
	for _, fb := range prog.FBDecls {
		if _, _, dup := ir.Lookup(l.types, fb.Name); !dup {
			l.types[fb.Name] = &ir.Type{Kind: ir.TypeFB, Name: fb.Name}
		}
	}
	return l.types, nil
}

// ErrFunctionBlockType is what ResolveTypeName's refusal of a
// function-block type wraps, standard (TON) or project-declared.
var ErrFunctionBlockType = errors.New("a function block — an instance is program state, not a tag")

// ResolveTypeName resolves a type as a manifest writes it — an IEC
// elementary type (`INT`, `REAL`, `TIME`, `STRING`), a TYPE
// the project declares (`Motor`), or an array of either
// (`ARRAY[1..4] OF REAL`) — against types, the project's TYPE table. A
// function-block type is refused: an FB instance is program state, never a
// tag.
func ResolveTypeName(name string, types map[string]*ir.Type) (*ir.Type, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("no type")
	}
	prog, err := Parse("PROGRAM __tagtype\nVAR\n    __t : " + name + ";\nEND_VAR\nEND_PROGRAM\n")
	if err != nil || len(prog.VarBlocks) != 1 || len(prog.VarBlocks[0].Variables) != 1 {
		return nil, fmt.Errorf("%q is not a type", name)
	}
	l := newLowerer(prog, nil)
	l.types = types
	if l.types == nil {
		l.types = map[string]*ir.Type{}
	}
	t, err := l.resolveType(prog.VarBlocks[0].Variables[0].Type)
	if err != nil {
		return nil, err
	}
	if t.Kind == ir.TypeFB {
		return nil, fmt.Errorf("%s is %w", name, ErrFunctionBlockType)
	}
	// An array of a standard block is a tag type (retain skips it). A
	// project block's entry from Types has no FBDef to build instances
	// from, so an array of one stays the unknown type it was.
	for e := t.Elem; e != nil; e = e.Elem {
		if e.Kind == ir.TypeFB && e.FB == nil {
			return nil, fmt.Errorf("unknown type %q", e.Name)
		}
	}
	return t, nil
}

// FileGlobal is one variable a file-level VAR_GLOBAL block (a GVL)
// declares.
type FileGlobal struct {
	Name     string
	Datatype string
	Constant bool
	Pos      Pos
}

// FileGlobals lists the globals a parsed source's file-level VAR_GLOBAL
// blocks declare, in source order.
func FileGlobals(prog *Program) []FileGlobal {
	var out []FileGlobal
	for _, vb := range prog.VarBlocks {
		if !vb.FileScope {
			continue
		}
		for _, vd := range vb.Variables {
			out = append(out, FileGlobal{Name: vd.Name, Datatype: vd.Datatype, Constant: vb.Constant, Pos: vd.Pos})
		}
	}
	return out
}
