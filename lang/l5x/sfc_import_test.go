package l5x

import (
	"strings"
	"testing"
)

// The docs say vendor SFC import is not supported (SFC-045): a Logix SFC
// routine is kept verbatim in the parse, never converted, and the ladder
// import names it as skipped rather than quietly dropping it.
func TestVendorSFCRoutineIsRefused(t *testing.T) {
	const src = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<RSLogix5000Content SchemaRevision="1.0" TargetName="Seq" TargetType="Program" ContainsContext="true">
<Controller Use="Context" Name="Plant">
<Programs Use="Context">
<Program Use="Target" Name="Seq" MainRoutineName="Chart">
<Routines>
<Routine Name="Chart" Type="SFC">
<SFCContent>
<Step ID="0" X="100" Y="100" Name="Step_000" InitialStep="true"/>
</SFCContent>
</Routine>
</Routines>
</Program>
</Programs>
</Controller>
</RSLogix5000Content>
`
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Ladder(f, LadderOptions{})
	if err == nil {
		t.Fatal("Ladder accepted an export holding only an SFC routine")
	}
	for _, want := range []string{"no RLL routines in this export", "Seq/Chart (SFC)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q is missing %q", err, want)
		}
	}
}
