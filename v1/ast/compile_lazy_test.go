// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"slices"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/types"
)

const lazyTestModule = `package test

f(x) := x if true

allow if {
	input.user == "alice"
	startswith(input.path, "/a")
	f(data.base.x)
}

allow if input.user == "bob"
`

func lazyTestSet(t *testing.T) (*Compiler, *LazyRuleSet) {
	t.Helper()
	c := MustCompileModules(map[string]string{"test.rego": lazyTestModule})
	s, err := c.Summarize(MustParseRef("data.test.allow"))
	if err != nil {
		t.Fatal(err)
	}
	return c, &LazyRuleSet{Summary: s, Rules: &copySource{rules: c.RuleTree.Find(s.Path).Values}}
}

func TestCompilerSummarize(t *testing.T) {
	_, set := lazyTestSet(t)
	s := set.Summary
	if !s.pkg().Equal(MustParseRef("data.test")) || types.Compare(s.Type, types.B) != 0 ||
		len(s.Deps) != 1 || !s.Deps[0].Equal(MustParseRef("data.test.f")) ||
		len(s.DataRefs) != 1 || !s.DataRefs[0].Equal(MustParseRef("data.base.x")) ||
		strings.Join(s.Builtins, ",") != "eq,startswith" {
		t.Errorf("unexpected summary %+v", s)
	}

	for _, tc := range []struct {
		note, module, other, path, err string
	}{
		{note: "rules below", module: "package test\np[x] := 1 if x := input.x\np.q := 2", path: "data.test.p", err: "rules below"},
		{note: "rules above", module: "package test\np[x] := 1 if x := input.x\np.q := 2", path: "data.test.p.q", err: "rules above"},
		{note: "general ref", module: "package test\np[x] := 1 if x := input.x", path: "data.test.p", err: "doesn't have the reference"},
		{note: "packages", module: "package test\na.b contains 1", other: "package test.a\nb contains 2", path: "data.test.a.b", err: "in packages"},
	} {
		t.Run(tc.note, func(t *testing.T) {
			modules := map[string]string{"test.rego": tc.module}
			if tc.other != "" {
				modules["other.rego"] = tc.other
			}
			if _, err := MustCompileModules(modules).Summarize(MustParseRef(tc.path)); err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("expected error %q, got %v", tc.err, err)
			}
		})
	}
}

func TestCompilerLazyRuleSet(t *testing.T) {
	full, set := lazyTestSet(t)
	c := NewCompiler().WithLazyRuleSet(set)
	c.Compile(map[string]*Module{"test.rego": MustParseModule(`package test

f(x) := x if true

decision := {"allow": allow}
`)})
	if c.Failed() {
		t.Fatal(c.Errors)
	}

	if tpe := c.TypeEnv.GetByRef(MustParseRef("data.test.decision")); types.Compare(tpe, types.NewObject([]*types.StaticProperty{types.NewStaticProperty("allow", types.B)}, nil)) != 0 {
		t.Errorf("expected decision to be typed from the lazy rule set, got %v", tpe)
	}
	result, err := c.RuleIndex(set.Summary.Path).Lookup(testResolver{input: MustParseTerm(`{"user": "bob"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rules) != 1 || !result.Rules[0].Equal(full.RuleTree.Find(set.Summary.Path).Values[1]) {
		t.Errorf("expected the second rule, got %v", result.Rules)
	}
	if !c.Required.ContainsBuiltin("startswith") {
		t.Error("expected the lazy rule set's builtins to be required")
	}
}

// A lazy rule set doesn't change the types of other rules: here, of a key that
// iterates a base document no rule is at.
func TestCompilerLazyRuleSetOtherTypes(t *testing.T) {
	const src = "package p\n\nr := [a, i] if {\n\tsome i\n\ta := data.w.parsed[input.u].rules[i].x\n}\n"
	full := MustCompileModules(map[string]string{"zz.rego": "package zz\n\nx := 1\n"})
	s, err := full.Summarize(MustParseRef("data.zz.x"))
	if err != nil {
		t.Fatal(err)
	}
	for _, lazy := range []bool{false, true} {
		c := NewCompiler()
		if lazy {
			c.WithLazyRuleSet(&LazyRuleSet{Summary: s, Rules: &copySource{rules: full.RuleTree.Find(s.Path).Values}})
		}
		if c.Compile(map[string]*Module{"p.rego": MustParseModule(src)}); c.Failed() {
			t.Fatal(c.Errors)
		}
		if got := c.TypeEnv.GetByRef(MustParseRef("data.p.r")); types.Compare(got, types.NewArray([]types.Type{types.A, types.A}, nil)) != 0 {
			t.Errorf("lazy=%v: expected array<any, any>, got %v", lazy, got)
		}
	}
}

func TestCompilerLazyRuleSetErrors(t *testing.T) {
	for _, tc := range []struct {
		note, module, other string
		caps                *Capabilities
		err                 string
	}{
		{note: "rules in modules too", module: "package test\nf(x) := x if true\nallow if false", err: "also given as a lazy rule set"},
		{note: "dependency gone", module: "package test\ng := 1", err: "depends on data.test.f, which has no rules"},
		{note: "recursion through a base document", module: "package test\nf(x) := x if true", other: "package base\nx := 1 if data.test.allow", err: "rego_recursion_error"},
		{note: "recursion", module: "package test\nf(x) := x if allow", err: "rego_recursion_error"},
		{
			note:   "unknown builtin",
			module: "package test\nf(x) := x if true",
			caps: func() *Capabilities {
				caps := CapabilitiesForThisVersion()
				caps.Builtins = slices.DeleteFunc(caps.Builtins, func(b *Builtin) bool { return b.Name == "startswith" })
				return caps
			}(),
			err: "calls startswith, which is not a built-in function",
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			_, set := lazyTestSet(t)
			modules := map[string]*Module{"test.rego": MustParseModule(tc.module)}
			if tc.other != "" {
				modules["other.rego"] = MustParseModule(tc.other)
			}
			c := NewCompiler().WithCapabilities(tc.caps).WithLazyRuleSet(set)
			if c.Compile(modules); !c.Failed() || !strings.Contains(c.Errors.Error(), tc.err) {
				t.Errorf("expected error %q, got %v", tc.err, c.Errors)
			}
		})
	}
}

// Lazy rule sets are compiled with print statements kept: a compiler dropping
// them erases the calls from the rules it reads.
func TestCompilerLazyRuleSetPrint(t *testing.T) {
	const src = "package test\n\nallow if {\n\tx := [1 | true]\n\tprint(input.user, count(x))\n\tinput.user == \"bob\"\n\tsome z in x\n\tprint([y | y := z])\n}\n\nallow if print(\"always\")\n"
	full := NewCompiler().WithEnablePrintStatements(true)
	if full.Compile(map[string]*Module{"test.rego": MustParseModule(src)}); full.Failed() {
		t.Fatal(full.Errors)
	}
	s, err := full.Summarize(MustParseRef("data.test.allow"))
	if err != nil {
		t.Fatal(err)
	}
	for _, prints := range []bool{false, true} {
		c := NewCompiler().WithEnablePrintStatements(prints).
			WithLazyRuleSet(&LazyRuleSet{Summary: s, Rules: &copySource{rules: full.RuleTree.Find(s.Path).Values}})
		if c.Compile(map[string]*Module{}); c.Failed() {
			t.Fatal(c.Errors)
		}
		result, err := c.RuleIndex(s.Path).Lookup(testResolver{input: MustParseTerm(`{"user": "bob"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rules) != 2 {
			t.Fatalf("print statements %v: expected both rules, got %v", prints, result.Rules)
		}
		bodies := []string{result.Rules[0].Body.String(), result.Rules[1].Body.String()}
		if !prints && !slices.Equal(bodies, []string{
			`__local0__ = [1 | true]; input.user = "bob"; __local3__ = __local0__[__local2__]`,
			"true",
		}) || prints && (!strings.Contains(bodies[0], "internal.print") || !strings.Contains(bodies[1], "internal.print")) {
			t.Errorf("print statements %v: got %v", prints, bodies)
		}
		set := c.LazyRuleSetOf(c.RuleTree.Find(s.Path).Values[0])
		if r, err := set.Rules.IndexRule(0); err != nil || strings.Contains(r.Body.String(), "internal.print") != prints {
			t.Errorf("print statements %v: expected the same of index rules, got %v (%v)", prints, r, err)
		}
	}
}
