// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package compiledrules

import (
	"slices"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
)

func TestKeyedStatements(t *testing.T) {
	popts := ast.ParserOptions{RegoVersion: ast.RegoV1, ProcessAnnotation: true, DropLocations: true}
	rule := func(id, body string) string {
		return "# METADATA\n# labels:\n#   id: \"" + id + "\"\nallow if { " + body + " }\n\n"
	}
	header := "package gen\n\nimport rego.v1\n\n"
	r1 := rule("r1", `input.x == "a"`)
	r2 := rule("r2", `input.x == "b"`)
	multi := "allow if {\n\t# inside\n\tinput.y == `{\n}`\n}\n"

	// The scan must key every statement like the parser keys the rule parsed from
	// it, or find no rule for it.
	tests := []struct {
		note    string
		src     string
		matched int // rules whose key the scan finds
	}{
		{note: "rules", src: header + r1 + r2 + multi + "allow if input.w\nallow if input.v\n", matched: 5},
		{note: "comment after the rule", src: header + r1[:len(r1)-1] + "# trailing\n\n" + r2, matched: 2},
		{note: "comment on the rule's line", src: header + "allow if input.w # c\n"},
		{note: "body", src: "package gen\n\nallow if {\n\tinput.x\n\tinput.y\n}\n", matched: 1},
		{note: "comment in body", src: "package gen\n\nallow if {\n\t# c\n\tinput.x\n}\n", matched: 1},
		{note: "braces in strings", src: "package gen\n\nallow if {\n\tinput.x == \"}\"\n\tinput.y == `{\n}`\n}\n", matched: 1},
		{note: "hash in string", src: "package gen\n\nallow if input.x == \"#\"\n", matched: 1},
		{note: "indented", src: "package gen\n\n  allow if input.x\n", matched: 1},
		{note: "metadata", src: "package gen\n\n# METADATA\n# title: t\n# labels:\n#   id: \"r1\"\nallow if input.x\n", matched: 1},
		{note: "crlf", src: "package gen\n\n# METADATA\r\n# title: t\r\nallow if {\r\n\tinput.x\r\n}\r\n", matched: 1},
		{note: "no space", src: "package gen\n\n# METADATA\n#title: t\nallow if input.x\n", matched: 1},
		{note: "blank comment", src: "package gen\n\n# METADATA\n# title: t\n#\n# description: d\nallow if input.x\n", matched: 1},
		{note: "assignment", src: "package gen\n\nallow := true if input.x\n", matched: 1},
		{note: "contains", src: "package gen\n\ndeny contains msg if {\n\tinput.x\n\tmsg := \"no\"\n}\n", matched: 1},
		{note: "ref head", src: "package gen\n\na.b.c if input.x\n", matched: 1},
		{note: "several", src: "package gen\n\nallow if input.x\n\nallow if input.y\n# METADATA\n# title: z\nallow if input.z\n", matched: 3},
		// To the parser, the indented comment ends the METADATA block.
		{note: "split block", src: "package gen\n\n# METADATA\n# a: 1\n  # b: 2\nallow if input.x\n"},
	}

	for _, tc := range tests {
		t.Run(tc.note, func(t *testing.T) {
			mod, err := ast.ParseModuleWithOpts("a.rego", tc.src, popts)
			if err != nil {
				t.Fatal(err)
			}
			var scanned []ast.RuleKey
			for _, key := range KeyedStatements([]byte(tc.src), true) {
				scanned = append(scanned, key)
			}
			matched := 0
			for _, r := range mod.Rules {
				if k, ok := r.SourceKey(); ok && slices.Contains(scanned, k) {
					matched++
				}
			}
			if matched != tc.matched {
				t.Fatalf("scan found the keys of %d rules, want %d", matched, tc.matched)
			}
		})
	}
}

func TestStatements(t *testing.T) {
	src := "package p\n\n# plain\n# METADATA\n# title: x\na if {\n\t`{`\n\t# c\n}\n# trailing\nb if \"}\"\n" +
		"c if {\n} else := 2 if {\n}\nelse := 3\n"
	var got []string
	for s := range statements([]byte(src)) {
		got = append(got, src[s.annotation:s.start]+"|"+src[s.start:s.end])
	}
	exp := []string{
		"|package p\n",
		"# METADATA\n# title: x\n|a if {\n\t`{`\n\t# c\n}\n",
		"|b if \"}\"\n",
		"|c if {\n} else := 2 if {\n}\nelse := 3\n",
	}
	if !slices.Equal(got, exp) {
		t.Fatalf("got %q, want %q", got, exp)
	}
}
