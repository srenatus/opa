// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
	"github.com/open-policy-agent/opa/v1/test/cases"
)

// TestRegoLazyRuleSets runs the test cases with every rule set the compiler can
// summarize kept out of memory.
func TestRegoLazyRuleSets(t *testing.T) {
	t.Parallel()

	for _, tc := range cases.MustLoad("../test/cases/testdata/v1").Sorted().Cases {
		t.Run("v1/"+tc.Note, func(t *testing.T) {
			t.Parallel()

			testRun(t, tc, regoVersion(ast.RegoV1), func(o *testOpts) {
				o.compile = compileLazily
			})
		})
	}
}

// TestPartialEvalLazyRuleSets runs the partial evaluation test cases with every
// rule set the compiler can summarize kept out of memory.
func TestPartialEvalLazyRuleSets(t *testing.T) {
	t.Parallel()
	runPartialEvalTestsWith(t, partialEvalTestCases(), compileModulesLazily)
}

// compileLazily compiles modules, and then again with every rule set the first
// compile can summarize given as a lazy rule set read from copies of its rules.
func compileLazily(t *testing.T, modules map[string]string, opts ast.CompileOpts) *ast.Compiler {
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
	return compileWithLazyRuleSets(t, full, parsed, c)
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
	return compileWithLazyRuleSets(t, full, copies(), ast.NewCompiler())
}

// compileWithLazyRuleSets compiles modules with c, with every rule set full can
// summarize left out of them and given as a lazy rule set instead.
func compileWithLazyRuleSets(t *testing.T, full *ast.Compiler, modules map[string]*ast.Module, c *ast.Compiler) *ast.Compiler {
	t.Helper()

	lazy := map[string]bool{}
	full.RuleTree.DepthFirst(func(node *ast.TreeNode) bool {
		if len(node.Values) == 0 {
			return false
		}
		path := node.Values[0].Ref().GroundPrefix()
		summary, err := full.Summarize(path)
		if err != nil {
			return false
		}
		c.WithLazyRuleSet(&ast.LazyRuleSet{Summary: summary, Rules: copiedRules(node.Values)})
		lazy[path.String()] = true
		return false
	})

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
		ast.CompileOpts{ParserOptions: ast.ParserOptions{RegoVersion: ast.RegoV1}})
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
