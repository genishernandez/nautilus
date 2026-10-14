package st

import "github.com/joyautomation/nautilus/lang/ir"

// FuncShadow is a function block instance declared with a FUNCTION's name.
type FuncShadow struct {
	Decl VarDecl // the instance's declaration, in own
	Func string  // the FUNCTION's declared spelling
}

// Message is the warning `naut check` and the editor show on the
// instance's declaration.
func (s FuncShadow) Message() string {
	return "instance " + s.Decl.Name + " shadows FUNCTION " + s.Func + " — " +
		s.Decl.Name + "(...) runs the instance here, never the function; rename one"
}

// InstancesShadowingFunctions lists the function block instances declared
// in own (in a PROGRAM, FUNCTION_BLOCK or FUNCTION) whose name is also a
// FUNCTION's, in own or in lib (the composed library, may be nil). Inside
// that POU the instance wins — `f(...)` runs the block, never the
// FUNCTION F (#336) — which is legal IEC scoping and rarely meant, so
// `naut check` and the editor warn. Blocks come first, then functions,
// then the program, each in declaration order.
func InstancesShadowingFunctions(own, lib *Program) []FuncShadow {
	if own == nil {
		return nil
	}
	funcs := map[string]string{}
	fbs := map[string]bool{}
	types := map[string]bool{}
	for _, p := range []*Program{lib, own} {
		if p == nil {
			continue
		}
		for _, fd := range p.FuncDecls {
			funcs[ir.NameKey(fd.Name)] = fd.Name
		}
		for _, fb := range p.FBDecls {
			fbs[ir.NameKey(fb.Name)] = true
		}
		for _, td := range p.TypeDecls {
			types[ir.NameKey(td.Name)] = true
		}
	}
	if len(funcs) == 0 {
		return nil
	}
	isFB := func(te TypeExpr) bool {
		nt, ok := te.(*NamedType)
		if !ok {
			return false
		}
		key := ir.NameKey(nt.Name)
		if types[key] {
			return false // a TYPE of that name resolves first
		}
		return ir.LookupFBType(nt.Name) != nil || fbs[key]
	}
	var out []FuncShadow
	scan := func(blocks []VarBlock) {
		for _, vb := range blocks {
			if vb.FileScope || vb.Kind == "VAR_GLOBAL" || vb.Kind == "VAR_EXTERNAL" {
				continue
			}
			for _, vd := range vb.Variables {
				if fn, ok := funcs[ir.NameKey(vd.Name)]; ok && isFB(vd.Type) {
					out = append(out, FuncShadow{Decl: vd, Func: fn})
				}
			}
		}
	}
	for _, fb := range own.FBDecls {
		scan(fb.VarBlocks)
	}
	for _, fd := range own.FuncDecls {
		scan(fd.VarBlocks)
	}
	scan(own.VarBlocks)
	return out
}
