// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package bundle

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/internal/compiledrules"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/version"
)

func generatedBundle(rules int) *Bundle {
	var src strings.Builder
	src.WriteString("package authz\n\n")
	for i := range rules {
		fmt.Fprintf(&src, "# METADATA\n# title: rule %d\nallow if { input.user == \"user-%d\"; startswith(input.path, \"/%d/\") }\n\n", i, i, i)
	}
	src.WriteString("deny if input.blocked\n")
	return &Bundle{
		Data:     map[string]any{},
		Manifest: Manifest{Generated: true},
		Modules: []ModuleFile{
			{Path: "/authz.rego", URL: "/authz.rego", Raw: []byte(src.String())},
			{Path: "/main.rego", URL: "/main.rego", Raw: []byte("package main\n\ndecision := {\"allow\": data.authz.allow}\n")},
		},
	}
}

// Functions without a body, like has(["rego", "v1"], _), are compiled as other
// rules are.
func TestCompileRulesFunctionsWithoutBody(t *testing.T) {
	b := generatedBundle(3)
	b.Modules = append(b.Modules, ModuleFile{Path: "/kw.rego", URL: "/kw.rego", Raw: []byte(`package kw

has(["future", "keywords"], _)
has(["rego", "v1"], _)
has(["future", "keywords", k], k)
`)})
	if err := b.compileRules(compileRulesOptions{minRules: 3}); err != nil {
		t.Fatal(err)
	}
	_, sets, err := compiledrules.Read(b.CompiledRules, moduleSources(b.Modules))
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 2 || !sets[1].Summary.Path.Equal(ast.MustParseRef("data.kw.has")) {
		t.Errorf("expected the rule sets data.authz.allow and data.kw.has, got %d", len(sets))
	}
}

func TestCompileRules(t *testing.T) {
	b := generatedBundle(5)
	if err := b.compileRules(compileRulesOptions{minRules: 3}); err != nil {
		t.Fatal(err)
	}
	key, sets, err := compiledrules.Read(b.CompiledRules, moduleSources(b.Modules))
	if err != nil {
		t.Fatal(err)
	}
	if exp := (compiledrules.Key{Version: version.Version, Modules: map[string]string{"/authz.rego": compiledrules.SourceHash(b.Modules[0].Raw)}}); !reflect.DeepEqual(key, exp) {
		t.Errorf("expected key %v, got %v", exp, key)
	}
	if len(sets) != 1 || !sets[0].Summary.Path.Equal(ast.MustParseRef("data.authz.allow")) {
		t.Fatalf("expected the rule set data.authz.allow, got %v", sets)
	}

	if len(compiledrules.Keys(sets[0])) != 5 {
		t.Fatalf("expected a key for each of 5 rules, got %d", len(compiledrules.Keys(sets[0])))
	}

	// What the reader finds when it scans the source.
	var found []ast.RuleKey
	for _, key := range compiledrules.KeyedStatements(b.Modules[0].Raw, true) {
		if key != (ast.RuleKey{}) {
			found = append(found, key)
		}
	}
	for _, k := range compiledrules.Keys(sets[0]) {
		if !slices.Contains(found, k) {
			t.Fatalf("key %x of a compiled rule isn't one the reader finds in the source", k)
		}
	}

	for _, tc := range []struct {
		note    string
		b       *Bundle
		compile func(*Bundle) error
	}{
		{note: "too few rules", b: generatedBundle(2), compile: func(b *Bundle) error { return b.compileRules(compileRulesOptions{minRules: 3}) }},
		{note: "default minimum", b: generatedBundle(5), compile: func(b *Bundle) error { return b.CompileRules(nil, nil) }},
	} {
		t.Run(tc.note, func(t *testing.T) {
			tc.b.CompiledRules = []byte("stale")
			if err := tc.compile(tc.b); err != nil {
				t.Fatal(err)
			}
			if tc.b.CompiledRules != nil {
				t.Errorf("expected no compiled rules, got %d bytes", len(tc.b.CompiledRules))
			}
		})
	}
}
