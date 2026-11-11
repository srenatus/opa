// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package bundle

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/internal/compiledrules"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/metrics"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
	"github.com/open-policy-agent/opa/v1/util"
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

func TestReadCompiledRules(t *testing.T) {
	write := func(t *testing.T, b *Bundle) *bytes.Buffer {
		t.Helper()
		var buf bytes.Buffer
		if err := NewWriter(&buf).Write(*b); err != nil {
			t.Fatal(err)
		}
		return &buf
	}
	compiled := func(t *testing.T, prints bool, builtBy string) *Bundle {
		t.Helper()
		if builtBy != "" {
			defer func(v string) { version.Version = v }(version.Version)
			version.Version = builtBy
		}
		b := generatedBundle(5)
		if prints {
			b.Modules[0].Raw = bytes.ReplaceAll(b.Modules[0].Raw, []byte("startswith("), []byte("print(input.user); startswith("))
		}
		if err := b.compileRules(compileRulesOptions{minRules: 3}); err != nil {
			t.Fatal(err)
		}
		return b
	}

	for _, tc := range []struct {
		note   string
		bundle func(*testing.T) *Bundle
		lazy   bool
		unused string // why the compiled rules aren't used
	}{
		{
			note:   "used",
			bundle: func(t *testing.T) *Bundle { return compiled(t, false, "") },
			lazy:   true,
		},
		{
			note:   "built by an older OPA",
			bundle: func(t *testing.T) *Bundle { return compiled(t, false, "0.1.0") },
			lazy:   true,
		},
		{
			note:   "built by a newer OPA",
			bundle: func(t *testing.T) *Bundle { return compiled(t, false, "99.0.0") },
			unused: "compiled by OPA 99.0.0, newer than this one",
		},
		{
			// The compiler erases the print calls, if it drops print statements.
			note:   "print calls",
			bundle: func(t *testing.T) *Bundle { return compiled(t, true, "") },
			lazy:   true,
		},
		{
			note: "source changed",
			bundle: func(t *testing.T) *Bundle {
				b := compiled(t, false, "")
				b.Modules[0].Raw = bytes.Replace(b.Modules[0].Raw, []byte(`"user-3"`), []byte(`"user-33"`), 1)
				return b
			},
			unused: "/authz.rego isn't the module they were compiled from",
		},
		{
			// The rules themselves are unchanged, but what they read isn't.
			note: "import changed",
			bundle: func(t *testing.T) *Bundle {
				b := generatedBundle(5)
				b.Modules[0].Raw = bytes.Replace(b.Modules[0].Raw, []byte("package authz\n"), []byte("package authz\n\nimport data.x as y\n"), 1)
				if err := b.compileRules(compileRulesOptions{minRules: 3}); err != nil {
					t.Fatal(err)
				}
				b.Modules[0].Raw = bytes.Replace(b.Modules[0].Raw, []byte("data.x"), []byte("data.z"), 1)
				return b
			},
			unused: "/authz.rego isn't the module they were compiled from",
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			m := metrics.New()
			b, err := NewReader(write(t, tc.bundle(t))).WithMetrics(m).WithCompiledRules().Read()
			if err != nil {
				t.Fatal(err)
			}
			if err := b.CompiledRulesUnused(); tc.unused == "" && err != nil || tc.unused != "" && (err == nil || !strings.Contains(err.Error(), tc.unused)) {
				t.Errorf("expected the compiled rules to be unused because %q, got %v", tc.unused, err)
			}
			if sets, rules := m.Counter(metricCompiledRuleSets).Value(), m.Counter(metricCompiledRules).Value(); tc.lazy && (sets != uint64(1) || rules != uint64(5)) || !tc.lazy && (sets != uint64(0) || rules != uint64(0)) {
				t.Errorf("expected metrics of the compiled rules used, got %v sets and %v rules", sets, rules)
			}
			var authz *ast.Module
			for _, mf := range b.Modules {
				if mf.Parsed.Package.Path.Equal(ast.MustParseRef("data.authz")) {
					authz = mf.Parsed
				}
			}
			if tc.lazy {
				if len(authz.Rules) != 1 || authz.Rules[0].Head.Name != "deny" {
					t.Errorf("expected only the deny rule to be parsed, got %v", authz.Rules)
				}
				if len(b.lazySets) != 1 {
					t.Errorf("expected one lazy rule set, got %d", len(b.lazySets))
				}
			} else {
				if len(authz.Rules) != 6 {
					t.Errorf("expected all 6 rules to be parsed, got %d", len(authz.Rules))
				}
				if len(b.lazySets) != 0 {
					t.Errorf("expected no lazy rule sets, got %d", len(b.lazySets))
				}
			}
		})
	}
}

func TestPoliciesFromStore(t *testing.T) {
	ctx := t.Context()
	b := generatedBundle(5)
	if err := b.compileRules(compileRulesOptions{minRules: 3}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		note   string
		change func([]byte) []byte
		lazy   bool
	}{
		{note: "stored rules", change: func(bs []byte) []byte { return bs }, lazy: true},
		{note: "policy changed", change: func(bs []byte) []byte { return bytes.Replace(bs, []byte(`"user-3"`), []byte(`"user-33"`), 1) }},
	} {
		t.Run(tc.note, func(t *testing.T) {
			store := inmem.New()
			txn := storage.NewTransactionOrDie(ctx, store, storage.WriteParams)
			defer store.Abort(ctx, txn)
			for _, mf := range b.Modules {
				if err := store.UpsertPolicy(ctx, txn, mf.Path, tc.change(mf.Raw)); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.(storage.CompiledRules).UpsertCompiledRules(ctx, txn, "authz", b.CompiledRules, []string{b.Modules[0].Path}); err != nil {
				t.Fatal(err)
			}

			// Without annotations processed, as the server parses policies.
			modules, sets, err := compiledrules.PoliciesFromStore(ctx, store, txn, ast.ParserOptions{})
			if err != nil {
				t.Fatal(err)
			}
			rules := len(modules["/authz.rego"].Rules)
			if tc.lazy && (len(sets) != 1 || rules != 1) || !tc.lazy && (len(sets) != 0 || rules != 6) {
				t.Errorf("expected lazy rule sets: %v, got %d sets and %d parsed rules", tc.lazy, len(sets), rules)
			}
		})
	}
}

func TestModuleWithLazyRules(t *testing.T) {
	ctx := t.Context()
	var src strings.Builder
	src.WriteString("# METADATA\n# title: authz\npackage authz\n\n")
	for i := range 4 {
		fmt.Fprintf(&src, "# METADATA\n# title: rule %d\nallow if input.user == \"user-%d\"\n\nx%d := %d\n\ndeny contains %d if input.n == %d\n\n", i, i, i, i, i, i)
	}

	for _, generated := range []bool{true, false} {
		t.Run(fmt.Sprintf("generated=%v", generated), func(t *testing.T) {
			b := &Bundle{
				Data:     map[string]any{},
				Manifest: Manifest{Generated: generated},
				Modules:  []ModuleFile{{Path: "/authz.rego", URL: "/authz.rego", Raw: []byte(src.String())}},
			}
			if err := b.compileRules(compileRulesOptions{minRules: 3}); err != nil {
				t.Fatal(err)
			}
			id := modulePathWithPrefix("authz", "/authz.rego")
			store := inmem.New()
			txn := storage.NewTransactionOrDie(ctx, store, storage.WriteParams)
			defer store.Abort(ctx, txn)
			if err := store.UpsertPolicy(ctx, txn, id, b.Modules[0].Raw); err != nil {
				t.Fatal(err)
			}
			if err := store.(storage.CompiledRules).UpsertCompiledRules(ctx, txn, "authz", b.CompiledRules, []string{id}); err != nil {
				t.Fatal(err)
			}

			// With annotations, as opa run parses policies.
			popts := ast.ParserOptions{ProcessAnnotation: true}
			if generated {
				popts = generatedParserOptions(popts)
			}
			modules, sets, err := compiledrules.PoliciesFromStore(ctx, store, txn, popts)
			if err != nil || len(sets) != 2 {
				t.Fatalf("expected two lazy rule sets, got %d (%v)", len(sets), err)
			}
			c := ast.NewCompiler()
			for _, s := range sets {
				c.WithLazyRuleSet(s)
			}
			if c.Compile(modules); c.Failed() {
				t.Fatal(c.Errors)
			}
			full := ast.MustCompileModulesWithOpts(map[string]string{id: src.String()}, ast.CompileOpts{ParserOptions: popts})

			got, err := compiledrules.ModuleWithLazyRules(c.Modules[id], b.Modules[0].Raw, c.LazyRuleSets())
			if err != nil {
				t.Fatal(err)
			}
			exp := full.Modules[id].Rules
			if len(got.Rules) != len(exp) {
				t.Fatalf("expected %d rules, got %d", len(exp), len(got.Rules))
			}
			if x, y := util.MustMarshalJSON(full.Modules[id].Annotations), util.MustMarshalJSON(got.Annotations); !bytes.Equal(x, y) {
				t.Errorf("expected the module's annotations\n%s\ngot\n%s", x, y)
			}
			for i, r := range got.Rules {
				if r.String() != exp[i].String() || (r.Location == nil) != generated || !generated && r.Location.Row != exp[i].Location.Row {
					t.Errorf("rule %d: expected %v at %v, got %v at %v", i, exp[i], exp[i].Location, r, r.Location)
				}
			}
		})
	}
}
