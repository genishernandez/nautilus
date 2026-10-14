package lsp

import (
	"encoding/json"
	"strings"
	"testing"
)

// #87: a keyword offered inside a comment or string rewrites the prose the
// user is typing ("while" becomes WHILE on the next space).
func TestNoCompletionInCommentsOrStrings(t *testing.T) {
	const uri = "file:///comment-completion.st"
	cases := []struct {
		name string
		text string // "|" marks the cursor
		want bool   // completion offered
	}{
		{"block comment", "PROGRAM P\nVAR a : INT; END_VAR\n(* keep wh| *)\nEND_PROGRAM\n", false},
		{"multi-line block comment", "PROGRAM P\nVAR a : INT; END_VAR\n(* first\n  re|\n*)\nEND_PROGRAM\n", false},
		{"nested block comment after inner close", "PROGRAM P\nVAR a : INT; END_VAR\n(* a (* b *) wh| *)\nEND_PROGRAM\n", false},
		{"line comment", "PROGRAM P\nVAR a : INT; END_VAR\na := 1; // wh|\nEND_PROGRAM\n", false},
		{"string literal", "PROGRAM P\nVAR s : STRING; END_VAR\ns := 'wh|';\nEND_PROGRAM\n", false},
		{"after a closed block comment", "PROGRAM P\nVAR a : INT; END_VAR\n(* note *) a|\nEND_PROGRAM\n", true},
		{"after a closed nested comment", "PROGRAM P\nVAR a : INT; END_VAR\n(* a (* b *) c *) a|\nEND_PROGRAM\n", true},
		{"line after a line comment", "PROGRAM P\nVAR a : INT; END_VAR\n// note\na|\nEND_PROGRAM\n", true},
		{"after a closed string", "PROGRAM P\nVAR s : STRING; END_VAR\ns := 'x'; s|\nEND_PROGRAM\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			idx := strings.IndexByte(c.text, '|')
			text := c.text[:idx] + c.text[idx+1:]
			pos := Position{Line: strings.Count(c.text[:idx], "\n"), Character: idx - (strings.LastIndexByte(c.text[:idx], '\n') + 1)}
			s := startSession(t)
			id := s.send("initialize", map[string]any{}, true)
			s.recvResponse(id)
			s.send("textDocument/didOpen", DidOpenTextDocumentParams{
				TextDocument: TextDocumentItem{URI: uri, LanguageID: "iec-st", Version: 1, Text: text},
			}, false)
			s.recv() // diagnostics
			id = s.send("textDocument/completion", TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: uri},
				Position:     pos,
			}, true)
			var items []CompletionItem
			if err := json.Unmarshal(s.recvResponse(id), &items); err != nil {
				t.Fatal(err)
			}
			if got := len(items) > 0; got != c.want {
				t.Errorf("%d items, offered = %v, want %v", len(items), got, c.want)
			}
		})
	}
}
