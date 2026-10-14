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
