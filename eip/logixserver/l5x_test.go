package logixserver

import (
	"reflect"
	"testing"

	"github.com/joyautomation/nautilus/lang/l5x"
)

// An AOI's template carries its local tags after its parameters, as a
// controller's does: RTU53's Flow_Totalizer keeps gettime : ARRAY [0..6] OF
// DINT as a local, and a browse of the real PLC sees it.
func TestSurfaceFromL5XAOILocals(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<RSLogix5000Content SchemaRevision="1.0" SoftwareRevision="38.01" TargetName="X" TargetType="Controller">
<Controller Name="X">
<AddOnInstructionDefinitions>
<AddOnInstructionDefinition Name="Get_TOD" Revision="1.0">
<Parameters>
<Parameter Name="EnableIn" TagType="Base" DataType="BOOL" Usage="Input" Required="false" Visible="false" ExternalAccess="Read Only"/>
<Parameter Name="EnableOut" TagType="Base" DataType="BOOL" Usage="Output" Required="false" Visible="false" ExternalAccess="Read Only"/>
<Parameter Name="Year" TagType="Base" DataType="DINT" Usage="Output" Required="false" Visible="true" ExternalAccess="Read Only"/>
</Parameters>
<LocalTags>
<LocalTag Name="gettime" DataType="DINT" Dimensions="7" Radix="Decimal" ExternalAccess="Read/Write">
<DefaultData Format="L5K"><![CDATA[[0,0,0,0,0,0,0]]]></DefaultData>
</LocalTag>
<LocalTag Name="grid" DataType="DINT" Dimensions="2 3" Radix="Decimal" ExternalAccess="Read/Write"/>
</LocalTags>
<Routines/>
</AddOnInstructionDefinition>
</AddOnInstructionDefinitions>
</Controller>
</RSLogix5000Content>
`
	f, err := l5x.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	s, err := SurfaceFromL5X(f, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Spec.Templates) != 1 {
		t.Fatalf("templates = %+v", s.Spec.Templates)
	}
	want := []MemberSpec{
		{Name: "EnableIn", Datatype: "BOOL"},
		{Name: "EnableOut", Datatype: "BOOL"},
		{Name: "Year", Datatype: "DINT"},
		{Name: "gettime", Datatype: "DINT", Dimension: 7},
	}
	if got := s.Spec.Templates[0].Members; !reflect.DeepEqual(got, want) {
		t.Errorf("members = %+v, want %+v", got, want)
	}
	if len(s.Skipped) != 1 {
		t.Errorf("skipped = %v, want the multi-dimensional local reported", s.Skipped)
	}
}
