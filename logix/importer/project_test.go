package importer

import (
	"sort"
	"strings"
	"testing"
)

// twoProgramExport is a whole-controller export with three ladder routines
// in two programs (Fill has two, Drain one), documented controller tags and
// an alias tag onto a rack point.
const twoProgramExport = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<RSLogix5000Content SchemaRevision="1.0" SoftwareRevision="38.01" TargetName="Tank" TargetType="Controller" ContainsContext="false" ExportDate="(pinned)">
<Controller Name="Tank" ProcessorType="1756-L85E" MajorRev="38" MinorRev="11">
<DataTypes/>
<Tags>
<Tag Name="StartPB" TagType="Base" DataType="BOOL" Radix="Decimal" Constant="false" ExternalAccess="Read/Write">
<Description>
<![CDATA[Fill start pushbutton]]>
</Description>
</Tag>
<Tag Name="FillValve" TagType="Base" DataType="BOOL" Radix="Decimal" Constant="false" ExternalAccess="Read/Write">
<Description>
<![CDATA[Inlet valve open command]]>
</Description>
</Tag>
<Tag Name="HiLevel" TagType="Base" DataType="BOOL" Radix="Decimal" Constant="false" ExternalAccess="Read/Write"/>
<Tag Name="DrainPump" TagType="Base" DataType="BOOL" Radix="Decimal" Constant="false" ExternalAccess="Read/Write"/>
<Tag Name="LevelSwitch" TagType="Alias" Radix="Decimal" AliasFor="Local:1:I.Data.4" ExternalAccess="Read/Write">
<Description>
<![CDATA[High level switch, rack slot 1 point 4]]>
</Description>
</Tag>
</Tags>
<Programs>
<Program Name="Fill" TestEdits="false" MainRoutineName="MainRoutine" Disabled="false" UseAsFolder="false">
<Tags/>
<Routines>
<Routine Name="MainRoutine" Type="RLL">
<RLLContent>
<Rung Number="0" Type="N">
<Text>
<![CDATA[XIC(StartPB)XIO(HiLevel)OTE(FillValve);]]>
</Text>
</Rung>
</RLLContent>
</Routine>
<Routine Name="Alarms" Type="RLL">
<RLLContent>
<Rung Number="0" Type="N">
<Text>
<![CDATA[XIC(FillValve)XIO(DrainPump)OTE(HiLevel);]]>
</Text>
</Rung>
</RLLContent>
</Routine>
</Routines>
</Program>
<Program Name="Drain" TestEdits="false" MainRoutineName="MainRoutine" Disabled="false" UseAsFolder="false">
<Tags/>
<Routines>
<Routine Name="MainRoutine" Type="RLL">
<RLLContent>
<Rung Number="0" Type="N">
<Text>
<![CDATA[XIC(HiLevel)OTE(DrainPump);]]>
</Text>
</Rung>
</RLLContent>
</Routine>
</Routines>
</Program>
</Programs>
<Tasks>
<Task Name="MainTask" Type="PERIODIC" Rate="50" Priority="10" Watchdog="500">
<ScheduledPrograms>
<ScheduledProgram Name="Fill"/>
<ScheduledProgram Name="Drain"/>
</ScheduledPrograms>
</Task>
</Tasks>
</Controller>
</RSLogix5000Content>
`

// LGA-022 / LGA-024: an export with several ladder routines comes in as one
// PROGRAM per routine, each a task in the manifest; controller tags keep
// their descriptions; an alias tag becomes a tag with alias:.
func TestEveryLadderRoutineBecomesAProgram(t *testing.T) {
	p := mustImport(t, []byte(twoProgramExport))
	if p.Routines != 3 || p.Complete != 3 {
		t.Errorf("routines %d (complete %d), want 3 of 3; notes: %v", p.Routines, p.Complete, p.Notes)
	}

	var programs []string
	for path, raw := range p.Files {
		if strings.HasSuffix(path, ".ld") && !strings.HasPrefix(path, "lib/") {
			programs = append(programs, path)
			if !strings.HasPrefix(strings.TrimSpace(stripComments(string(raw))), "PROGRAM ") {
				t.Errorf("%s is not a PROGRAM:\n%s", path, raw)
			}
		}
	}
	sort.Strings(programs)
	if len(programs) != 3 {
		t.Fatalf("ladder program files = %v, want one per routine (3)", programs)
	}
	man := string(p.Files["nautilus.yaml"])
	for _, want := range []string{"target:\n  logix:\n", "controller: Tank\n", "processor: 1756-L85E\n", `revision: "38.11"`} {
		if !strings.Contains(man, want) {
			t.Errorf("manifest's target is not filled from the controller (%q):\n%s", want, man)
		}
	}
	for _, prog := range programs {
		if !strings.Contains(man, "program: "+prog) {
			t.Errorf("manifest has no task for %s:\n%s", prog, man)
		}
	}
	// Each routine's own rung, in its own program.
	all := ""
	for _, prog := range programs {
		all += string(p.Files[prog])
	}
	for _, rung := range []string{"StartPB /HiLevel ( FillValve )", "FillValve /DrainPump ( HiLevel )", "HiLevel ( DrainPump )"} {
		if strings.Count(all, rung) != 1 {
			t.Errorf("want the rung %q in exactly one program:\n%s", rung, all)
		}
	}

	tags := string(p.Files["tags/logix.yaml"])
	for _, want := range []string{
		`desc: "Fill start pushbutton"`,
		`desc: "Inlet valve open command"`,
		`alias: "Local:1:I.Data.4"`,
	} {
		if !strings.Contains(tags, want) {
			t.Errorf("tag file missing %s:\n%s", want, tags)
		}
	}
	for _, l := range strings.Split(tags, "\n") {
		if strings.Contains(l, "name: LevelSwitch") && !strings.Contains(l, `alias: "Local:1:I.Data.4"`) {
			t.Errorf("the alias tag is not a tag with alias: %s", l)
		}
	}
}

// stripComments drops the (* ... *) header an imported file opens with.
func stripComments(s string) string {
	for {
		i := strings.Index(s, "(*")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "*)")
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+2:]
	}
}
