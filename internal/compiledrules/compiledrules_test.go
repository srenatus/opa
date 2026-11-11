// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package compiledrules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/test/cases"
	"github.com/open-policy-agent/opa/v1/types"
)

// corpusSets returns every rule set of the v1 test cases that can be summarized,
// and the sources of their modules.
func corpusSets(t *testing.T) ([]Set, Sources) {
	t.Helper()
	var sets []Set
	src := map[string][]byte{}
	for n, tc := range cases.MustLoad("../../v1/test/cases/testdata/v1").Sorted().Cases {
		modules := map[string]string{}
		for i, m := range tc.Modules {
			name := fmt.Sprintf("case-%d/test-%d.rego", n, i)
			modules[name] = m
			src[name] = []byte(m)
		}
		popts := ast.ParserOptions{RegoVersion: ast.RegoV1, ProcessAnnotation: true}
		if tc.ExperimentalKeywords {
			popts.Capabilities = ast.CapabilitiesForThisVersion(ast.CapabilitiesExperimentalKeywords(true))
		}
		c, err := ast.CompileModulesWithOpt(modules, ast.CompileOpts{ParserOptions: popts})
		if err != nil {
			continue
		}
		c.RuleTree.DepthFirst(func(node *ast.TreeNode) bool {
			if len(node.Values) == 0 {
				return false
			}
			if s, err := c.Summarize(node.Values[0].Ref().GroundPrefix()); err == nil {
				keys := make([]ast.RuleKey, 0, len(node.Values))
				for _, r := range node.Values {
					if k, ok := r.SourceKey(); ok {
						keys = append(keys, k)
					}
				}
				if len(keys) != len(node.Values) {
					keys = nil
				}
				sets = append(sets, Set{Summary: s, Rules: node.Values, Keys: keys})
			}
			return false
		})
	}
	return sets, mapSources(src)
}

func mapSources(m map[string][]byte) Sources {
	return func(file string) []byte { return m[file] }
}

func TestRoundTrip(t *testing.T) {
	sets, sources := corpusSets(t)
	if len(sets) < 1000 {
		t.Fatalf("expected the corpus to give a few thousand rule sets, got %d", len(sets))
	}

	var buf bytes.Buffer
	want := Key{Version: "1.2.3"}
	if err := Write(&buf, want, sets, sources); err != nil {
		t.Fatal(err)
	}
	key, got, err := Read(buf.Bytes(), sources)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(key, want) {
		t.Errorf("expected key %v, got %v", want, key)
	}
	if len(got) != len(sets) {
		t.Fatalf("expected %d rule sets, got %d", len(sets), len(got))
	}

	rules, keyed := 0, 0
	for i, set := range sets {
		want, have := set.Summary, got[i].Summary
		if !have.Path.Equal(want.Path) || !have.Head.Equal(want.Head) || types.Compare(have.Type, want.Type) != 0 ||
			!slices.EqualFunc(have.Deps, want.Deps, refEqual) || !slices.EqualFunc(have.DataRefs, want.DataRefs, refEqual) ||
			!slices.Equal(have.Builtins, want.Builtins) {
			t.Fatalf("summary of %v: expected %+v, got %+v", want.Path, want, have)
		}

		if !slices.Equal(Keys(got[i]), set.Keys) {
			t.Fatalf("rule set %v: expected keys %v, got %v", want.Path, set.Keys, Keys(got[i]))
		}
		if set.Keys != nil {
			keyed++
		}
		if n := got[i].Rules.Len(); n != len(set.Rules) {
			t.Fatalf("rule set %v: expected %d rules, got %d", want.Path, len(set.Rules), n)
		}
		for j, r := range set.Rules {
			dec, err := got[i].Rules.Rule(j)
			if err != nil {
				t.Fatal(err)
			}
			for a, b := r, dec; a != nil || b != nil; a, b = a.Else, b.Else {
				if a == nil || b == nil || !a.Equal(b) || a.Default != b.Default || !sameAnnotations(a, b) {
					t.Fatalf("rule set %v, rule %d: expected\n%v\ngot\n%v", want.Path, j, r, dec)
				}
			}
			// What the index is built from: the rule without locations or annotations.
			bare, err := got[i].Rules.IndexRule(j)
			if err != nil {
				t.Fatal(err)
			}
			for a, b := withoutAnnotations(r), bare; a != nil || b != nil; a, b = withoutAnnotations(a.Else), b.Else {
				if a == nil || b == nil || !a.Equal(b) || b.Location != nil || b.Annotations != nil {
					t.Fatalf("rule set %v, rule %d: expected to index\n%v\ngot\n%v", want.Path, j, r, bare)
				}
			}
			if !dec.Module.Package.Path.Equal(r.Module.Package.Path) {
				t.Fatalf("rule set %v: expected package %v, got %v", want.Path, r.Module.Package.Path, dec.Module.Package.Path)
			}
			rules++
		}
	}
	if keyed == 0 {
		t.Error("expected rule sets with keys")
	}
	t.Logf("%d rule sets (%d with keys), %d rules, %d bytes", len(sets), keyed, rules, buf.Len())
}

func TestTruncated(t *testing.T) {
	src := `package test

f(x) := y if { y := {k: v | some k, v in x} }
p contains [x, y] if { some x in input.xs; y := f(x); every z in input.zs { z != x } }
`
	c := ast.MustCompileModules(map[string]string{"test.rego": src})
	sources := mapSources(map[string][]byte{"test.rego": []byte(src)})
	var sets []Set
	for _, path := range []string{"data.test.f", "data.test.p"} {
		s, err := c.Summarize(ast.MustParseRef(path))
		if err != nil {
			t.Fatal(err)
		}
		sets = append(sets, Set{Summary: s, Rules: c.RuleTree.Find(ast.MustParseRef(path)).Values})
	}
	var buf bytes.Buffer
	if err := Write(&buf, Key{Version: "1.0.0"}, sets, sources); err != nil {
		t.Fatal(err)
	}

	bs := buf.Bytes()
	for n := range len(bs) {
		_, got, err := Read(bs[:n], sources)
		if err != nil {
			continue
		}
		if len(got) == len(sets) {
			t.Fatalf("expected reading %d of %d bytes to fail", n, len(bs))
		}
	}
}

func TestKey(t *testing.T) {
	key := Key{Version: "1.22.0", Modules: map[string]string{"/p.rego": "00ff"}}
	for _, tc := range []struct {
		version string
		usable  bool
	}{
		{version: "1.22.0", usable: true},
		{version: "1.23.0", usable: true},
		{version: "2.0.0", usable: true},
		{version: "1.21.9"},
		{version: "1.22.0-dev"},
	} {
		if got := key.Usable(tc.version); got != tc.usable {
			t.Errorf("OPA %v: expected usable %v, got %v", tc.version, tc.usable, got)
		}
	}

	var buf bytes.Buffer
	if err := Write(&buf, key, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadKey(buf.Bytes()); err != nil || !reflect.DeepEqual(got, key) {
		t.Fatalf("expected key %v, got %v, %v", key, got, err)
	}
	other := slices.Clone(buf.Bytes())
	other[len(magic)-1]++
	if _, err := ReadKey(other); err == nil {
		t.Error("expected an encoding of another format version to be refused")
	}
}

func refEqual(a, b ast.Ref) bool { return a.Equal(b) }

// withoutAnnotations returns a copy of r without its annotations, nil for nil.
func withoutAnnotations(r *ast.Rule) *ast.Rule {
	if r == nil {
		return nil
	}
	cpy := *r
	cpy.Annotations = nil
	return &cpy
}

// sameAnnotations compares the annotations of rules as JSON, which their labels
// differ in the Go types of.
func sameAnnotations(a, b *ast.Rule) bool {
	x, err1 := json.Marshal(a.Annotations)
	y, err2 := json.Marshal(b.Annotations)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// BenchmarkDecodeRule decodes one of 1000 generated rules of four conditions
// each, with and without locations, and for comparison from JSON.
func BenchmarkDecodeRule(b *testing.B) {
	var src, labeled strings.Builder
	src.WriteString("package authz\n\n")
	labeled.WriteString("package authz\n\n")
	for i := range 1000 {
		rule := fmt.Sprintf("allow if { input.account == \"account-%d\"; input.action == \"objects:get\"; startswith(input.resource, \"store:%d:\"); input.role == \"role-%d\" }\n", i, i%10, i)
		src.WriteString(rule)
		fmt.Fprintf(&labeled, "# METADATA\n# labels:\n#   id: \"rule-%d\"\n#   version: \"1\"\n%s", i, rule)
	}
	compiled := func(src string, dropLocations bool) []*ast.Rule {
		c := ast.MustCompileModulesWithOpts(map[string]string{"authz.rego": src}, ast.CompileOpts{
			ParserOptions: ast.ParserOptions{DropLocations: dropLocations, ProcessAnnotation: true},
		})
		return c.RuleTree.Find(ast.MustParseRef("data.authz.allow")).Values
	}
	encoded := func(b *testing.B, src string, rules []*ast.Rule) {
		c := ast.MustCompileModules(map[string]string{"authz.rego": src})
		s, err := c.Summarize(ast.MustParseRef("data.authz.allow"))
		if err != nil {
			b.Fatal(err)
		}
		var buf bytes.Buffer
		sources := mapSources(map[string][]byte{"authz.rego": []byte(src)})
		if err := Write(&buf, Key{Version: "1.0.0"}, []Set{{Summary: s, Rules: rules}}, sources); err != nil {
			b.Fatal(err)
		}
		_, sets, err := Read(buf.Bytes(), sources)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(buf.Len())/float64(len(rules)), "B/rule")
		b.ReportAllocs()
		for i := range b.N {
			if _, err := sets[0].Rules.Rule(i % len(rules)); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.Run("encoded", func(b *testing.B) { encoded(b, src.String(), compiled(src.String(), true)) })
	b.Run("encoded+locations", func(b *testing.B) { encoded(b, src.String(), compiled(src.String(), false)) })
	b.Run("encoded+labels", func(b *testing.B) { encoded(b, labeled.String(), compiled(labeled.String(), true)) })

	b.Run("json", func(b *testing.B) {
		rules := compiled(src.String(), true)
		pkg, err := json.Marshal(rules[0].Module.Package)
		if err != nil {
			b.Fatal(err)
		}
		docs := make([][]byte, len(rules))
		size := 0
		for i, r := range rules {
			bs, err := json.Marshal(r)
			if err != nil {
				b.Fatal(err)
			}
			docs[i] = []byte(`{"package":` + string(pkg) + `,"rules":[` + string(bs) + `]}`)
			size += len(bs)
		}
		b.ReportMetric(float64(size)/float64(len(rules)), "B/rule")
		b.ReportAllocs()
		for i := range b.N {
			var m ast.Module
			if err := json.Unmarshal(docs[i%len(docs)], &m); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestRoundTripLocations(t *testing.T) {
	sets, sources := corpusSets(t)
	var buf bytes.Buffer
	if err := Write(&buf, Key{Version: "1.0.0"}, sets, sources); err != nil {
		t.Fatal(err)
	}
	_, got, err := Read(buf.Bytes(), sources)
	if err != nil {
		t.Fatal(err)
	}
	locs := func(r *ast.Rule) []*ast.Location {
		var ls []*ast.Location
		ast.WalkNodes(r, func(n ast.Node) bool {
			if _, args := n.(ast.Args); !args { // empty, or nil once decoded
				ls = append(ls, n.Loc())
			}
			return false
		})
		for _, a := range r.Annotations {
			ls = append(ls, a.Location)
		}
		return ls
	}
	n := 0
	for i, set := range sets {
		for j, r := range set.Rules {
			dec, err := got[i].Rules.Rule(j)
			if err != nil {
				t.Fatal(err)
			}
			want, have := locs(r), locs(dec)
			if len(want) != len(have) {
				t.Fatalf("%v: expected %d nodes, got %d", r, len(want), len(have))
			}
			for k := range want {
				w, h := want[k], have[k]
				if (w == nil) != (h == nil) || w != nil && (w.File != h.File || w.Row != h.Row || w.Col != h.Col ||
					w.Offset != h.Offset || !bytes.Equal(w.Text, h.Text) || !slices.Equal(w.Tabs, h.Tabs)) {
					t.Fatalf("%v: node %d: expected location %+v, got %+v", r, k, w, h)
				}
				if w != nil {
					n++
				}
			}
		}
	}
	t.Logf("%d locations", n)
}

// The METADATA of a rule comes before it, and is a span of the source too.
func TestAnnotationLocationIsSpan(t *testing.T) {
	src := "package p\n\n# METADATA\n# title: a rule with a long title that should not be stored twice\nallow if input.x == 1\n"
	c := ast.MustCompileModulesWithOpts(map[string]string{"p.rego": src}, ast.CompileOpts{ParserOptions: ast.ParserOptions{ProcessAnnotation: true}})
	s, err := c.Summarize(ast.MustParseRef("data.p.allow"))
	if err != nil {
		t.Fatal(err)
	}
	rules := c.RuleTree.Find(s.Path).Values
	sources := func(string) []byte { return []byte(src) }
	var buf bytes.Buffer
	if err := Write(&buf, Key{}, []Set{{Summary: s, Rules: rules}}, sources); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("# METADATA")) {
		t.Error("expected the METADATA's location as a span, not its text")
	}
	_, sets, err := Read(buf.Bytes(), sources)
	if err != nil {
		t.Fatal(err)
	}
	r, err := sets[0].Rules.Rule(0)
	if err != nil {
		t.Fatal(err)
	}
	want, got := rules[0].Annotations[0].Location, r.Annotations[0].Location
	if got == nil || got.Row != want.Row || got.Col != want.Col || got.Offset != want.Offset || !bytes.Equal(got.Text, want.Text) {
		t.Errorf("expected location %+v, got %+v", want, got)
	}
}

func TestSourcesDiffer(t *testing.T) {
	src := "package test\n\np if input.x == 1\n"
	c := ast.MustCompileModules(map[string]string{"test.rego": src})
	s, err := c.Summarize(ast.MustParseRef("data.test.p"))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	sets := []Set{{Summary: s, Rules: c.RuleTree.Find(s.Path).Values}}
	if err := Write(&buf, Key{Version: "1.0.0"}, sets, mapSources(map[string][]byte{"test.rego": []byte(src)})); err != nil {
		t.Fatal(err)
	}
	for _, other := range []map[string][]byte{nil, {"test.rego": []byte(src + "\n")}} {
		if _, _, err := Read(buf.Bytes(), mapSources(other)); err == nil {
			t.Errorf("expected reading with sources %q to fail", other)
		}
	}
}

// fullIndexRules builds the index from rules decoded in full, for comparison.
type fullIndexRules struct{ ast.LazyRules }

func (f fullIndexRules) IndexRule(i int) (*ast.Rule, error) { return f.Rule(i) }

// BenchmarkLazyIndexBuild compiles a lazy rule set of 1000 rules of four
// conditions, each with labels, decoded with locations: the build decodes each
// rule once, bare or in full.
func BenchmarkLazyIndexBuild(b *testing.B) {
	var src strings.Builder
	src.WriteString("package authz\n\n")
	for i := range 1000 {
		fmt.Fprintf(&src, "# METADATA\n# labels:\n#   id: \"rule-%d\"\n#   version: \"1\"\nallow if { input.account == \"account-%d\"; input.action == \"objects:get\"; startswith(input.resource, \"store:%d:\"); input.role == \"role-%d\" }\n", i, i, i%10, i)
	}
	c := ast.MustCompileModulesWithOpts(map[string]string{"authz.rego": src.String()}, ast.CompileOpts{
		ParserOptions: ast.ParserOptions{ProcessAnnotation: true},
	})
	s, err := c.Summarize(ast.MustParseRef("data.authz.allow"))
	if err != nil {
		b.Fatal(err)
	}
	var buf bytes.Buffer
	sources := mapSources(map[string][]byte{"authz.rego": []byte(src.String())})
	if err := Write(&buf, Key{Version: "1.0.0"}, []Set{{Summary: s, Rules: c.RuleTree.Find(s.Path).Values}}, sources); err != nil {
		b.Fatal(err)
	}
	_, sets, err := Read(buf.Bytes(), sources)
	if err != nil {
		b.Fatal(err)
	}

	for _, bare := range []bool{true, false} {
		rules := sets[0].Rules
		if !bare {
			rules = fullIndexRules{rules}
		}
		b.Run(fmt.Sprintf("bare=%v", bare), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				lc := ast.NewCompiler().WithLazyRuleSet(&ast.LazyRuleSet{Summary: s, Rules: rules})
				if lc.Compile(map[string]*ast.Module{}); lc.Failed() {
					b.Fatal(lc.Errors)
				}
			}
		})
	}
}
