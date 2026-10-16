package writer

// The hardware configuration (logix-authoring.md §6, decision 1; #275):
// the controller's chassis, its I/O modules and the adapters and modules
// beyond it, which the alias tags of an imported program land on. Logix
// Designer keeps it in the project's <Modules> section, and a project
// carries it as an L5X of its own (target.logix.hardware, hardware.L5X by
// `naut logix import`): the writer copies that section into the project
// as it is, in place of the Local module it would otherwise generate,
// and invents nothing — a module's configuration, connections and
// identity are Logix Designer's own.
//
// An alias of a rack point is checked against it at write time
// (logix/io-module): Local:1:I.Data.3 needs a module in slot 1 of the
// controller's chassis, Remote1:1:I.Data.0 one in slot 1 under the
// adapter Remote1, Drive1:I.Speed a module named Drive1. Without the
// module the SDK imports the project and then fails the build with
// RxTAG_ALIAS_TARGET_NOT_FOUND, naming no tag.

import (
	"fmt"
	"strings"

	"github.com/joyautomation/nautilus/lang/l5x"
)

// Hardware is a project's I/O configuration: the <Modules> section of an
// L5X, and the modules in it.
type Hardware struct {
	// File names the L5X it came from, for diagnostics.
	File string
	// Modules are the modules, the controller's own Local module among
	// them; XML is the <Modules> element as the file has it.
	Modules []*l5x.Module
	XML     string
}

// ParseHardware reads the hardware configuration from an L5X: a Logix
// Designer export of the whole project, or the hardware-only file
// `naut logix import` writes. Only its <Modules> section is used.
func ParseHardware(file string, raw []byte) (*Hardware, error) {
	f, err := l5x.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	c := f.Controller
	if c == nil || strings.TrimSpace(c.ModulesXML) == "" {
		return nil, fmt.Errorf("%s: no <Modules> section; the hardware configuration is the <Modules> of a Logix Designer export", file)
	}
	h := &Hardware{File: file, Modules: c.Modules, XML: c.ModulesXML}
	if h.local() == nil {
		return nil, fmt.Errorf("%s: its <Modules> has no Local module (the controller's own); export the whole project from Logix Designer", file)
	}
	return h, nil
}

func (h *Hardware) local() *l5x.Module {
	for _, m := range h.Modules {
		if m.Name == "Local" {
			return m
		}
	}
	return nil
}

// modulesXML is the section as the writer emits it: the file's own
// bytes with its line ends made the writer's (XML reads CRLF as LF, so
// nothing changes).
func (h *Hardware) modulesXML() string {
	return strings.ReplaceAll(strings.TrimSpace(h.XML), "\r\n", "\n")
}

// checkHardware runs the hardware rules: the configuration's Local module
// is the target's controller, and every alias of a rack point names a
// module the configuration has.
func (lw *lowered) checkHardware() {
	h := lw.opts.Hardware
	if h != nil {
		lm := h.local()
		switch {
		case !strings.EqualFold(lm.CatalogNumber, lw.opts.ProcessorType):
			lw.diag(ruleHardware, 0, "", "%s: its Local module is a %s, and the target's processor is %s; set target.logix.processor to %s, or export the hardware configuration from a %s project",
				h.File, lm.CatalogNumber, lw.opts.ProcessorType, lm.CatalogNumber, lw.opts.ProcessorType)
		case lm.Major != lw.opts.MajorRev || lm.Minor != lw.opts.MinorRev:
			lw.diag(ruleHardware, 0, "", "%s: its Local module is at revision %s.%s, and the target's revision is %s.%s; set target.logix.revision to \"%s.%s\", or export the hardware configuration from a project at %s.%s",
				h.File, lm.Major, lm.Minor, lw.opts.MajorRev, lw.opts.MinorRev, lm.Major, lm.Minor, lw.opts.MajorRev, lw.opts.MinorRev)
		}
	}
	for _, t := range lw.ctrlTags {
		if t.Alias == "" {
			continue
		}
		parent, slot, ok := RackPoint(t.Alias)
		if !ok {
			continue
		}
		if msg := h.missing(parent, slot); msg != "" {
			lw.diag(ruleIOModule, t.Line, "", "%s is an alias of %s, %s", t.Name, t.Alias, msg)
		}
	}
}

// RackPoint splits an alias target that is a module's I/O point into the
// module it names: Local:1:I.Data.3 is slot "1" under "Local" (the
// controller's chassis), Remote1:1:I.Data.0 slot "1" under the adapter
// Remote1, Drive1:I.Speed the module "Drive1" itself (slot ""). ok is
// false for an alias of a tag.
func RackPoint(target string) (parent, slot string, ok bool) {
	base := target
	if i := strings.IndexAny(base, ".["); i >= 0 {
		base = base[:i]
	}
	parts := strings.Split(base, ":")
	switch {
	case len(parts) == 3 && parts[0] != "" && isDigits(parts[1]):
		return parts[0], parts[1], true
	case len(parts) == 2 && parts[0] != "" && !strings.EqualFold(parts[0], "Program"):
		return parts[0], "", true
	}
	return "", "", false
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// missing is "" when the configuration has the module a rack point names,
// and otherwise the end of the diagnostic: what is missing and what to do.
func (h *Hardware) missing(parent, slot string) string {
	what := "the module " + parent
	if slot != "" {
		what = fmt.Sprintf("the module in slot %s of %s", slot, parent)
		if strings.EqualFold(parent, "Local") {
			what = fmt.Sprintf("the module in slot %s of the controller's chassis (Local)", slot)
		}
	}
	if h == nil {
		return "a point of " + what + ", and the project has no hardware configuration to carry it: set target.logix.hardware to an L5X export of the project with its I/O modules (`naut logix import --project` writes one, hardware.L5X), or the Logix build fails with RxTAG_ALIAS_TARGET_NOT_FOUND"
	}
	byName := map[string]*l5x.Module{}
	for _, m := range h.Modules {
		byName[strings.ToLower(m.Name)] = m
	}
	if slot == "" {
		if byName[strings.ToLower(parent)] != nil {
			return ""
		}
		return fmt.Sprintf("and %s has no module named %s; it has %s. Add the module in Logix Designer and export the project over %s, or correct the alias", h.File, parent, h.inventory(), h.File)
	}
	if !strings.EqualFold(parent, "Local") && byName[strings.ToLower(parent)] == nil {
		return fmt.Sprintf("and %s has no adapter named %s; it has %s. Add it in Logix Designer and export the project over %s, or correct the alias", h.File, parent, h.inventory(), h.File)
	}
	for _, m := range h.Modules {
		if strings.EqualFold(m.ParentModule, parent) && !strings.EqualFold(m.Name, parent) && m.Slot() == slot {
			return ""
		}
	}
	return fmt.Sprintf("and %s has no %s; it has %s. Add the module in Logix Designer and export the project over %s, or correct the alias", h.File, strings.TrimPrefix(what, "the "), h.inventory(), h.File)
}

// inventory lists the configuration's modules for a diagnostic, each
// where an alias would name it: Local:1 DI_Slot1 (1756-IB16).
func (h *Hardware) inventory() string {
	var out []string
	for _, m := range h.Modules {
		if m.Name == "Local" {
			continue
		}
		at := m.Name
		if s := m.Slot(); isDigits(s) {
			at = m.ParentModule + ":" + s + " " + m.Name
		}
		out = append(out, fmt.Sprintf("%s (%s)", at, m.CatalogNumber))
	}
	if len(out) == 0 {
		return "only the controller"
	}
	return strings.Join(out, ", ")
}
