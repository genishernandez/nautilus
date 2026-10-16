package sparkplug

import (
	"github.com/joyautomation/nautilus/lang/ir"
	"github.com/joyautomation/nautilus/sparkplug/spb"
)

// UDT struct tags publish as Sparkplug Template instances (TemplateRef → type
// name). A host like Ignition needs the matching Template *definition* to
// interpret them, so NBIRTH carries one definition metric per distinct struct
// type — name = type name, value = a Template with IsDefinition=true and
// member name+datatype (no values), nested types included.

// templateDefs returns definition metrics for every distinct struct type
// reachable from the published tags in snap, deduped by type name and ordered
// dependencies-first so a nested type is defined before the type that uses it.
// Caller holds n.mu (reads rbeFor).
func (n *Node) templateDefs(snap map[string]ir.Value, ts uint64) []Metric {
	seen := map[string]bool{}
	var order []*ir.StructDef
	var walk func(sd *ir.StructDef)
	walk = func(sd *ir.StructDef) {
		if sd == nil || seen[sd.Name] {
			return
		}
		seen[sd.Name] = true
		for _, f := range sd.Fields {
			if f.Type != nil && f.Type.Kind == ir.TypeStruct {
				walk(f.Type.Struct) // define nested types first
			}
		}
		order = append(order, sd)
	}
	for _, name := range sortedNames(snap) {
		if _, ok := n.rbeFor(name); !ok {
			continue
		}
		if v := snap[name]; v.Kind == ir.TypeStruct {
			walk(v.Struct)
		}
	}

	defs := make([]Metric, 0, len(order))
	for _, sd := range order {
		defs = append(defs, Metric{
			Name:      sd.Name,
			Datatype:  spb.DataType_Template,
			Timestamp: ts,
			Value:     definitionTemplate(sd),
		})
	}
	return defs
}

// definitionTemplate builds a Template definition for a struct type. Scalar
// members carry name + datatype and no value (IsNull). A member that is
// itself a struct carries no scalar value either, but instead of IsNull it
// carries a Template value with TemplateRef set to the nested type's name
// (IsDefinition left false) — a reference to that type's own definition
// metric, which templateDefs emits alongside this one (dependency-ordered:
// nested types before their users). This is the convention Ignition/Tahu use
// for a UDT member that is itself a UDT: the member is a reference, not an
// inline copy of the nested definition.
//
// An array member's datatype is the array datatype itself (Int64Array, …) —
// Sparkplug 3.0 §6.4.14 "Template" lets a template's metrics be any datatype — with no
// value, like any other definition member: its length is in the instances.
// A member Sparkplug cannot carry (publishable: an array of structs, a
// multi-dimensional array) is left out here and in every instance.
func definitionTemplate(sd *ir.StructDef) *Template {
	t := &Template{IsDefinition: true}
	for _, f := range sd.Fields {
		if !publishable(f.Type) {
			continue
		}
		m := Metric{Name: f.Name, Datatype: definitionDatatype(f.Type)}
		if f.Type != nil && f.Type.Kind == ir.TypeStruct && f.Type.Struct != nil {
			m.Value = &Template{TemplateRef: f.Type.Struct.Name}
		} else {
			m.IsNull = true // definition members have no value
		}
		t.Metrics = append(t.Metrics, m)
	}
	return t
}

// definitionDatatype maps a field's ir.Type to its Sparkplug datatype for a
// definition member — the datatype MetricFromValue gives an instance's member
// of that type. Nested structs are Template; an array of scalars is the
// matching array datatype.
func definitionDatatype(t *ir.Type) spb.DataType {
	if t == nil {
		return spb.DataType_String
	}
	switch t.Kind {
	case ir.TypeBool:
		return spb.DataType_Boolean
	case ir.TypeInt, ir.TypeTime:
		return spb.DataType_Int64
	case ir.TypeReal:
		return spb.DataType_Double
	case ir.TypeString:
		return spb.DataType_String
	case ir.TypeStruct:
		return spb.DataType_Template
	case ir.TypeArray:
		if t.Elem != nil {
			if dt, ok := arrayDatatype(t.Elem.Kind); ok {
				return dt
			}
		}
	}
	return spb.DataType_String
}
