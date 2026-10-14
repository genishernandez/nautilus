package project

import (
	"strings"
	"testing"
)

// Tag-model: a function-block type is refused as a tag type, including a
// FUNCTION_BLOCK the project itself declares (not only a standard one).
func TestProjectFunctionBlockIsRefusedAsTagType(t *testing.T) {
	err := newRuntimeErr(t, map[string]string{
		"nautilus.yaml": "tasks:\n  - program: main.st\ntags:\n  - { name: Pump1Seq, role: state, type: Sequencer }\n",
		"lib.st":        "FUNCTION_BLOCK Sequencer\nVAR_INPUT\n    Go : BOOL;\nEND_VAR\nEND_FUNCTION_BLOCK\n",
		"main.st":       "PROGRAM Main\nEND_PROGRAM\n",
	})
	if err == nil || !strings.Contains(err.Error(), "Sequencer") {
		t.Fatalf("err = %v, want the project's function-block type refused as a tag type", err)
	}
}
