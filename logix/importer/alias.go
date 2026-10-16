package importer

import (
	"strings"

	"github.com/joyautomation/nautilus/lang/l5x"
)

// Alias tags (#330). Logix exports an alias tag with no DataType: the tag
// is AliasFor="Target" and its type is whatever the target is. The import
// types it through the target, declares it like any controller tag the
// logic names, and the tag file keeps the binding as alias: — the I/O
// as alias tags design in docs/design/logix-authoring.md, where the
// manifest is the one place a binding lives.
//
// A target is a base tag, a member of a UDT, an Add-On Instruction
// instance, a TIMER or a COUNTER, an array element, a bit of an integer
// (Status.5), or a rack point. A rack point's bit (Local:1:I.Data.3) is a
// BOOL whatever the module; any other rack point is typed by the module's
// definition, which an export does not carry, so an alias of one is
// refused by name. So is an alias of a whole block instance: a nautilus
// block instance is a program's, never a binding.

// aliasDepth bounds an alias of an alias.
const aliasDepth = 8

// aliasType types a controller alias tag through its AliasFor target.
func (im *importer) aliasType(t *l5x.Tag) (dtype, dims string, err *refusal) {
	return im.aliasTarget(t, 0)
}

func (im *importer) aliasTarget(t *l5x.Tag, depth int) (string, string, *refusal) {
	target := strings.TrimSpace(t.AliasFor)
	if strings.Contains(target, ":") {
		if _, bit := splitBitOperand(target); bit {
			return "BOOL", "", nil
		}
		return "", "", refuse("alias", "%s is an alias of the module I/O point %s, and the export has no module definition to type it", t.Name, target)
	}
	base := baseOf(target)
	bt := im.ctrl[strings.ToLower(base)]
	if base == "" || bt == nil {
		return "", "", refuse("alias", "%s is an alias of %s, which is not in the export", t.Name, target)
	}
	dtype, dims := bt.DataType, bt.Dimensions
	if bt.AliasFor != "" {
		if depth >= aliasDepth {
			return "", "", refuse("alias", "%s: an alias chain more than %d deep", t.Name, aliasDepth)
		}
		var err *refusal
		if dtype, dims, err = im.aliasTarget(bt, depth+1); err != nil {
			return "", "", err
		}
	}
	bad := func(format string, a ...any) (string, string, *refusal) {
		return "", "", refuse("alias", "%s is an alias of %s: "+format, append([]any{t.Name, target}, a...)...)
	}
	for rest := target[len(base):]; rest != ""; {
		switch rest[0] {
		case '[':
			j := strings.IndexByte(rest, ']')
			if j < 0 {
				return bad("an unclosed index")
			}
			if dims == "" {
				return bad("%s is not an array", strings.TrimSuffix(target, rest))
			}
			if strings.Contains(rest[1:j], ",") || strings.Contains(strings.TrimSpace(dims), " ") {
				return bad("a multi-dimensional array")
			}
			dims, rest = "", rest[j+1:]
		case '.':
			j := 1
			for j < len(rest) && rest[j] != '.' && rest[j] != '[' {
				j++
			}
			name := rest[1:j]
			rest = rest[j:]
			if dims != "" {
				return bad("a member of a whole array")
			}
			if name != "" && strings.Trim(name, "0123456789") == "" {
				// Status.5: a bit of an integer.
				if !bitHost[strings.ToUpper(dtype)] || rest != "" {
					return bad("a %s has no bit %s", dtype, name)
				}
				dtype = "BOOL"
				continue
			}
			mt, md, ok := im.memberType(dtype, name)
			if !ok {
				return bad("a %s has no member %s", dtype, name)
			}
			dtype, dims = mt, md
		default:
			return bad("an unreadable target")
		}
	}
	up := strings.ToUpper(dtype)
	if a := im.aois[strings.ToLower(dtype)]; up == "TIMER" || up == "COUNTER" || a != nil {
		kind := up
		if a != nil {
			kind = "Add-On Instruction " + a.Name
		}
		return "", "", refuse("alias", "%s is an alias of the %s %s; nautilus has no alias of a block instance", t.Name, kind, target)
	}
	return dtype, dims, nil
}

// bitHost are the types a .N bit selects from.
var bitHost = map[string]bool{"SINT": true, "INT": true, "DINT": true, "LINT": true, "USINT": true, "UINT": true, "UDINT": true, "ULINT": true}

var timerMembers = map[string]string{"PRE": "DINT", "ACC": "DINT", "EN": "BOOL", "TT": "BOOL", "DN": "BOOL"}
var counterMembers = map[string]string{"PRE": "DINT", "ACC": "DINT", "CU": "BOOL", "CD": "BOOL", "DN": "BOOL", "OV": "BOOL", "UN": "BOOL"}

// memberType is the type of member name of a structure: a TIMER's or a
// COUNTER's, an Add-On Instruction's parameter, or a UDT's member (a BIT
// member is a BOOL).
func (im *importer) memberType(dtype, name string) (string, string, bool) {
	up := strings.ToUpper(dtype)
	switch up {
	case "TIMER":
		t, ok := timerMembers[strings.ToUpper(name)]
		return t, "", ok
	case "COUNTER":
		t, ok := counterMembers[strings.ToUpper(name)]
		return t, "", ok
	}
	if a := im.aois[strings.ToLower(dtype)]; a != nil {
		for _, p := range a.Parameters {
			if strings.EqualFold(p.Name, name) {
				return p.DataType, dimText(p.Dimension), true
			}
		}
		return "", "", false
	}
	for _, dt := range im.file.Controller.DataTypes {
		if !strings.EqualFold(dt.Name, dtype) {
			continue
		}
		for _, m := range dt.Members {
			if m.Hidden || !strings.EqualFold(m.Name, name) {
				continue
			}
			if strings.EqualFold(m.DataType, "BIT") {
				return "BOOL", "", true
			}
			return m.DataType, dimText(m.Dimension), true
		}
	}
	return "", "", false
}
