package project

import (
	"strings"
	"testing"
)

// Tag-model: a function-block type is refused as a tag type, including a
// FUNCTION_BLOCK the project itself declares (not only a standard one),
// and with the same message: the name is a block, not an unknown type.
func TestProjectFunctionBlockIsRefusedAsTagType(t *testing.T) {
	cases := map[string]string{
		"Sequencer": "tag Pump1Seq: Sequencer is a function block",
		"TON":       "tag Pump1Seq: TON is a function block",
	}
	for typ, want := range cases {
		err := newRuntimeErr(t, map[string]string{
			"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: Pump1Seq, role: state, type: \"" + typ + "\" }\n",
			"lib.st":        "FUNCTION_BLOCK Sequencer\nVAR_INPUT\n    Go : BOOL;\nEND_VAR\nEND_FUNCTION_BLOCK\n",
			"main.st":       "PROGRAM Main\nEND_PROGRAM\n",
		})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("type %s: err = %v, want %q", typ, err, want)
			continue
		}
		if strings.Contains(err.Error(), "neither an IEC elementary type") {
			t.Errorf("type %s: err = %v, want the function-block message, not the unknown-type one", typ, err)
		}
	}
}

// Tag-model: a type that holds a function block is refused as a tag type
// too — an array of a standard or a project block, at any nesting, and a
// STRUCT with a block member — with the function-block message, naming
// the block, not the unknown-type one.
func TestTypeHoldingAFunctionBlockIsRefusedAsTagType(t *testing.T) {
	const lib = "TYPE Station : STRUCT\n    Delay : TON;\n    Count : INT;\nEND_STRUCT;\nEND_TYPE\n" +
		"TYPE Line : STRUCT\n    Stations : ARRAY[1..2] OF Station;\nEND_STRUCT;\nEND_TYPE\n" +
		"TYPE Delays : ARRAY[1..3] OF TON;\nEND_TYPE\n" +
		"FUNCTION_BLOCK Sequencer\nVAR_INPUT\n    Go : BOOL;\nEND_VAR\nEND_FUNCTION_BLOCK\n"
	cases := map[string]string{
		"ARRAY[1..2] OF TON":                "tag Pump1Seq: ARRAY[1..2] OF TON — TON is a function block; an instance is program state, not a tag",
		"ARRAY[1..2] OF Sequencer":          "tag Pump1Seq: ARRAY[1..2] OF Sequencer — Sequencer is a function block; an instance is program state, not a tag",
		"ARRAY[1..2] OF ARRAY[1..3] OF CTU": "tag Pump1Seq: ARRAY[1..2] OF ARRAY[1..3] OF CTU — CTU is a function block",
		"ARRAY[1..2, 1..3] OF Sequencer":    "— Sequencer is a function block",
		"Delays":                            "tag Pump1Seq: Delays — TON is a function block",
		"Station":                           "tag Pump1Seq: Station — member Delay is a TON, a function block; an instance is program state, not a tag",
		"Line":                              "tag Pump1Seq: Line — member Stations.Delay is a TON, a function block",
		"ARRAY[1..4] OF Station":            "— member Delay is a TON, a function block",
	}
	for typ, want := range cases {
		err := newRuntimeErr(t, map[string]string{
			"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: Pump1Seq, role: setpoint, type: \"" + typ + "\" }\n",
			"lib.st":        lib,
			"main.st":       "PROGRAM Main\nEND_PROGRAM\n",
		})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("type %s: err = %v, want %q", typ, err, want)
			continue
		}
		if strings.Contains(err.Error(), "neither an IEC elementary type") || strings.Contains(err.Error(), "unknown type") {
			t.Errorf("type %s: err = %v, want the function-block message, not the unknown-type one", typ, err)
		}
	}
	// What a block does not reach stays a tag type.
	for _, typ := range []string{"ARRAY[1..2] OF INT", "ARRAY[1..2] OF ARRAY[1..3] OF REAL"} {
		if err := newRuntimeErr(t, map[string]string{
			"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: Pump1Seq, role: setpoint, type: \"" + typ + "\" }\n",
			"lib.st":        lib,
			"main.st":       "PROGRAM Main\nEND_PROGRAM\n",
		}); err != nil {
			t.Errorf("type %s: %v", typ, err)
		}
	}
}
