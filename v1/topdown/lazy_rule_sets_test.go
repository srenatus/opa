// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/internal/compiledrules"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
	"github.com/open-policy-agent/opa/v1/test/cases"
)

// TestRegoLazyRuleSets runs the test cases with every rule set the compiler can
// summarize kept out of memory: read from copies of its rules, and decoded from
// an encoding of them.
func TestRegoLazyRuleSets(t *testing.T) {
	t.Parallel()

	for _, encoded := range []bool{false, true} {
		for _, tc := range cases.MustLoad("../test/cases/testdata/v1").Sorted().Cases {
			name := "copied/v1/" + tc.Note
			if encoded {
				name = "encoded/v1/" + tc.Note
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				testRun(t, tc, regoVersion(ast.RegoV1), func(o *testOpts) {
					o.compile = func(t *testing.T, modules map[string]string, opts ast.CompileOpts) *ast.Compiler {
						return compileLazily(t, modules, opts, encoded)
					}
				})
			})
		}
	}
}

// TestPartialEvalLazyRuleSets runs the partial evaluation test cases with every
// rule set the compiler can summarize kept out of memory.
func TestPartialEvalLazyRuleSets(t *testing.T) {
	t.Parallel()
	runPartialEvalTestsWith(t, partialEvalTestCases(), compileModulesLazily)
}

// compileLazily compiles modules, and then again with every rule set the first
// compile can summarize given as a lazy rule set: read from copies of its rules,
// or from an encoding of the rule sets if encoded.
func compileLazily(t testing.TB, modules map[string]string, opts ast.CompileOpts, encoded bool) *ast.Compiler {
	t.Helper()

	full := ast.MustCompileModulesWithOpts(modules, opts)
	parsed := make(map[string]*ast.Module, len(modules))
	for name, src := range modules {
		mod, err := ast.ParseModuleWithOpts(name, src, opts.ParserOptions)
		if err != nil {
			t.Fatal(err)
		}
		parsed[name] = mod
	}
	c := ast.NewCompiler().
		WithCapabilities(opts.ParserOptions.Capabilities).
		WithDefaultRegoVersion(opts.ParserOptions.RegoVersion)
	var sources compiledrules.Sources
	if encoded {
		raw := make(map[string][]byte, len(modules))
		for name, src := range modules {
			raw[name] = []byte(src)
		}
		sources = func(file string) []byte { return raw[file] }
	}
	return compileWithLazyRuleSets(t, full, parsed, c, sources)
}

// compileModulesLazily is compileLazily for parsed modules.
func compileModulesLazily(t *testing.T, modules map[string]*ast.Module, _ ast.ParserOptions) *ast.Compiler {
	t.Helper()

	copies := func() map[string]*ast.Module {
		cpy := make(map[string]*ast.Module, len(modules))
		for name, mod := range modules {
			cpy[name] = mod.Copy()
		}
		return cpy
	}
	full := ast.NewCompiler()
	if full.Compile(copies()); full.Failed() {
		t.Fatal(full.Errors)
	}
	return compileWithLazyRuleSets(t, full, copies(), ast.NewCompiler(), nil)
}

// compileWithLazyRuleSets compiles modules with c, with every rule set full can
// summarize left out of them and given as a lazy rule set instead, decoded from
// an encoding of the rule sets if there are sources.
func compileWithLazyRuleSets(t testing.TB, full *ast.Compiler, modules map[string]*ast.Module, c *ast.Compiler, sources compiledrules.Sources) *ast.Compiler {
	t.Helper()

	var sets []*ast.LazyRuleSet
	full.RuleTree.DepthFirst(func(node *ast.TreeNode) bool {
		if len(node.Values) == 0 {
			return false
		}
		path := node.Values[0].Ref().GroundPrefix()
		summary, err := full.Summarize(path)
		if err != nil {
			return false
		}
		sets = append(sets, &ast.LazyRuleSet{Summary: summary, Rules: copiedRules(node.Values)})
		return false
	})

	if sources != nil {
		var buf bytes.Buffer
		enc := make([]compiledrules.Set, len(sets))
		for i, set := range sets {
			enc[i] = compiledrules.Set{Summary: set.Summary, Rules: set.Rules.(copiedRules)}
		}
		if err := compiledrules.Write(&buf, compiledrules.Key{}, enc, sources); err != nil {
			t.Fatal(err)
		}
		_, decoded, err := compiledrules.Read(buf.Bytes(), sources)
		if err != nil {
			t.Fatal(err)
		}
		sets = decoded
	}
	return compileWithSets(t, sets, modules, c)
}

// compileWithSets compiles modules with sets, leaving out the rules they hold.
func compileWithSets(t testing.TB, sets []*ast.LazyRuleSet, modules map[string]*ast.Module, c *ast.Compiler) *ast.Compiler {
	t.Helper()

	lazy := make(map[string]bool, len(sets))
	for _, set := range sets {
		c.WithLazyRuleSet(set)
		lazy[set.Summary.Path.String()] = true
	}
	for _, mod := range modules {
		mod.Rules = slices.DeleteFunc(mod.Rules, func(rule *ast.Rule) bool {
			return lazy[rule.Ref().GroundPrefix().String()]
		})
	}
	if c.Compile(modules); c.Failed() {
		t.Fatalf("lazy compile: %v", c.Errors)
	}
	return c
}

// copiedRules hands out copies of compiled rules, as a source decoding them would.
type copiedRules []*ast.Rule

func (r copiedRules) Len() int { return len(r) }

func (r copiedRules) Rule(i int) (*ast.Rule, error) {
	cpy := r[i].Copy()
	cpy.Module = r[i].Module
	return cpy, nil
}

func (r copiedRules) IndexRule(i int) (*ast.Rule, error) { return r.Rule(i) }

func TestLazyRuleSetPlaceholderFails(t *testing.T) {
	c := compileLazily(t, map[string]string{"test.rego": "package test\n\nallow if input.x\n"},
		ast.CompileOpts{ParserOptions: ast.ParserOptions{RegoVersion: ast.RegoV1}}, false)
	rules := c.GetRulesExact(ast.MustParseRef("data.test.allow"))
	if len(rules) != 1 || c.LazyRuleSetOf(rules[0]) == nil {
		t.Fatalf("expected the lazy rule set's placeholder, got %v", rules)
	}
	_, err := NewQuery(rules[0].Body).WithCompiler(c).WithStore(inmem.New()).Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "lazy_rule_set_placeholder") {
		t.Fatalf("expected evaluating the placeholder's body to fail, got %v", err)
	}
}

// countedRules counts the rules read from it.
type countedRules struct {
	copiedRules
	reads *int
}

func (r countedRules) Rule(i int) (*ast.Rule, error) {
	*r.reads++
	return r.copiedRules.Rule(i)
}

func (r countedRules) IndexRule(i int) (*ast.Rule, error) {
	*r.reads++
	return r.copiedRules.IndexRule(i)
}

// Partial evaluation asks the rules an index lookup finds whether they need
// saving; a rule set reached through another rule is asked one rule at a time,
// stopping at the first that does. Either way, it reads few rules.
func TestPartialEvalLazyRuleSetReadsFew(t *testing.T) {
	var src strings.Builder
	src.WriteString("package test\n\n")
	for i := range 100 {
		fmt.Fprintf(&src, "allow if { input.x == %d; input.y == 1 }\n", i)
	}
	modules := map[string]string{"test.rego": src.String(), "q.rego": "package q\n\np if data.test.allow\n"}
	full := ast.MustCompileModules(modules)
	s, err := full.Summarize(ast.MustParseRef("data.test.allow"))
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	lazy := ast.NewCompiler().WithLazyRuleSet(&ast.LazyRuleSet{Summary: s, Rules: countedRules{copiedRules(full.RuleTree.Find(s.Path).Values), &reads}})
	if lazy.Compile(map[string]*ast.Module{"q.rego": ast.MustParseModule(modules["q.rego"])}); lazy.Failed() {
		t.Fatal(lazy.Errors)
	}

	partial := func(c *ast.Compiler, query string) string {
		queries, _, err := NewQuery(ast.MustParseBody(query)).WithCompiler(c).WithStore(inmem.New()).
			WithInput(ast.MustParseTerm(`{"x": 5}`)).WithUnknowns([]*ast.Term{ast.MustParseTerm("input.y")}).PartialRun(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(queries)
	}
	for _, tc := range []struct {
		query string
		reads int
	}{
		{"data.test.allow", 1},
		{"data.q.p", 2},
	} {
		exp := partial(full, tc.query)
		reads = 0
		if got := partial(lazy, tc.query); got != exp {
			t.Errorf("%s: expected %v, got %v", tc.query, exp, got)
		}
		if reads != tc.reads {
			t.Errorf("%s: expected %d reads, got %d", tc.query, tc.reads, reads)
		}
	}
}

// A lazy rule set may read a base document that has rules by now: evaluation
// reads them, as it would for rules compiled from source.
func TestLazyRuleSetBaseDocumentWithRules(t *testing.T) {
	a := "package a\n\nallow if data.b.x == 1\n\nallow if input.y\n"
	full := ast.MustCompileModules(map[string]string{"a.rego": a})
	s, err := full.Summarize(ast.MustParseRef("data.a.allow"))
	if err != nil {
		t.Fatal(err)
	}
	b := ast.MustParseModule("package b\n\nx := 1\n")
	lazy := ast.NewCompiler().WithLazyRuleSet(&ast.LazyRuleSet{Summary: s, Rules: copiedRules(full.RuleTree.Find(s.Path).Values)})
	if lazy.Compile(map[string]*ast.Module{"a.rego": ast.MustParseModule("package a\n"), "b.rego": b}); lazy.Failed() {
		t.Fatal(lazy.Errors)
	}
	store := inmem.New()
	txn := storage.NewTransactionOrDie(t.Context(), store)
	defer store.Abort(t.Context(), txn)
	rs, err := NewQuery(ast.MustParseBody("data.a.allow = x")).WithCompiler(lazy).WithStore(store).WithTransaction(txn).Run(t.Context())
	if err != nil || len(rs) != 1 || !rs[0][ast.Var("x")].Equal(ast.BooleanTerm(true)) {
		t.Fatalf("expected allow to read the rule at data.b.x, got %v (%v)", rs, err)
	}
}

// BenchmarkLazyRuleSetEval evaluates 1000 rules compiled in full and decoded from
// an encoding: indexed, the lookup returns one rule; unindexed, the index can't
// narrow the rules and all are evaluated.
func BenchmarkLazyRuleSetEval(b *testing.B) {
	for _, tc := range []struct {
		note, rule, input string
	}{
		{note: "indexed", rule: "allow if input.user == \"u%d\"\n", input: `{"user": "u500"}`},
		{note: "unindexed", rule: "allow if count(input.roles) > %d\n", input: `{"roles": []}`},
	} {
		var src strings.Builder
		src.WriteString("package test\n\n")
		for i := range 1000 {
			fmt.Fprintf(&src, tc.rule, i)
		}
		modules := map[string]string{"test.rego": src.String()}
		opts := ast.CompileOpts{ParserOptions: ast.ParserOptions{RegoVersion: ast.RegoV1}}
		query := ast.MustParseBody("data.test.allow")
		input := ast.MustParseTerm(tc.input)

		for _, lazy := range []bool{false, true} {
			c := ast.MustCompileModulesWithOpts(modules, opts)
			if lazy {
				c = compileLazily(b, modules, opts, true)
				if len(c.LazyRuleSets()) != 1 {
					b.Fatal("expected a lazy rule set")
				}
			}
			eval := func() error {
				_, err := NewQuery(query).WithCompiler(c).WithStore(inmem.New()).WithInput(input).Run(b.Context())
				return err
			}
			name := fmt.Sprintf("%s/lazy=%v", tc.note, lazy)
			b.Run(name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if err := eval(); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run(name+"/parallel", func(b *testing.B) {
				b.ReportAllocs()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						if err := eval(); err != nil {
							b.Error(err)
							return
						}
					}
				})
			})
		}
	}
}

func TestLazyRuleSetEvaluatedLabels(t *testing.T) {
	modules := map[string]string{"test.rego": `package test

# METADATA
# labels:
#   id: r1
#   version: "1"
allow if input.user == "alice"

# METADATA
# labels:
#   id: r2
#   version: "2"
allow if input.user == "bob"
`}
	opts := ast.CompileOpts{ParserOptions: ast.ParserOptions{RegoVersion: ast.RegoV1, ProcessAnnotation: true}}
	labels := func(c *ast.Compiler, partial bool) string {
		tracker := &EvaluatedRuleTracker{}
		q := NewQuery(ast.MustParseBody("data.test.allow")).WithCompiler(c).WithStore(inmem.New()).
			WithInput(ast.MustParseTerm(`{"user": "bob"}`)).WithEvaluatedRuleTracker(tracker)
		var err error
		if partial {
			_, _, err = q.WithUnknowns([]*ast.Term{ast.MustParseTerm("input")}).PartialRun(t.Context())
		} else {
			_, err = q.Run(t.Context())
		}
		if err != nil {
			t.Fatal(err)
		}
		bs, err := json.Marshal(tracker.Labels)
		if err != nil {
			t.Fatal(err)
		}
		return string(bs)
	}
	full := ast.MustCompileModulesWithOpts(modules, opts)
	lazy := compileLazily(t, modules, opts, true)
	if len(lazy.LazyRuleSets()) != 1 {
		t.Fatal("expected a lazy rule set")
	}
	for _, partial := range []bool{false, true} {
		if exp, got := labels(full, partial), labels(lazy, partial); exp != got || exp == "null" {
			t.Errorf("partial %v: expected labels %s, got %s", partial, exp, got)
		}
	}
}

// TestLazyRuleSetLanguageFeatures evaluates rule sets using the newer language
// features compiled in full and decoded from an encoding.
func TestLazyRuleSetLanguageFeatures(t *testing.T) {
	modules := map[string]string{"test.rego": `package test

import future.keywords.not

logical if input.a and input.b
logical if input.c or input.d

every_ if every x in input.xs { x > 0 }

not_body if not {
	input.a
	input.b
}

some_in contains x if some x in input.xs

with_ if count(input.xs) == 0 with input.xs as []

template := $"a={input.a}, xs={count(input.xs)}"

comprehension := {x: y | some x, y in input.xs}

else_ := 1 if input.a
else := 2 if input.b
else := 3

default def := "none"

def := "a" if input.a

f(x) := x * 2 if x > 0

func_ := f(count(input.xs))
`}
	opts := ast.CompileOpts{ParserOptions: ast.ParserOptions{RegoVersion: ast.RegoV1, FutureKeywords: []string{"and", "or"}}}
	full := ast.MustCompileModulesWithOpts(modules, opts)
	lazy := compileLazily(t, modules, opts, true)
	if n := len(lazy.LazyRuleSets()); n < 10 {
		t.Fatalf("expected the rule sets to be lazy, got %d", n)
	}
	for _, input := range []string{
		`{"a": true, "b": true, "xs": [1, 2]}`,
		`{"a": false, "b": true, "c": true, "xs": [0, 3]}`,
		`{"b": false, "d": true, "xs": []}`,
		`{}`,
	} {
		eval := func(c *ast.Compiler) string {
			store := inmem.New()
			txn := storage.NewTransactionOrDie(t.Context(), store)
			defer store.Abort(t.Context(), txn)
			rs, err := NewQuery(ast.MustParseBody("x = data.test")).WithCompiler(c).WithStore(store).WithTransaction(txn).
				WithInput(ast.MustParseTerm(input)).Run(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			return fmt.Sprint(rs)
		}
		if exp, got := eval(full), eval(lazy); exp != got {
			t.Errorf("input %s: expected\n%s\ngot\n%s", input, exp, got)
		}
	}
}
