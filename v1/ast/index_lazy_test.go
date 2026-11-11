// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"errors"
	"maps"
	"slices"
	"testing"
)

// copySource hands out copies of rules, as a source decoding them would, and
// records what it is asked for.
type copySource struct {
	rules      []*Rule
	reads      []int
	indexReads []int
	err        error
}

func (s *copySource) Len() int { return len(s.rules) }

func (s *copySource) Rule(i int) (*Rule, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.reads = append(s.reads, i)
	return s.rules[i].Copy(), nil
}

func (s *copySource) IndexRule(i int) (*Rule, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.indexReads = append(s.indexReads, i)
	return s.rules[i].Copy(), nil
}

// assertLazyAgrees checks that a lazy index over rules answers like want, the
// eager index's answer: from Lookup, or from AllRules if all.
func assertLazyAgrees(t *testing.T, isVirtual func(Ref) bool, rules []*Rule, all bool, resolver ValueResolver, want *IndexResult) {
	t.Helper()

	index, err := newLazyIndex(isVirtual, &copySource{rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	var got *IndexResult
	if all {
		got, err = index.AllRules(resolver)
	} else {
		got, err = index.Lookup(resolver)
	}
	if err != nil {
		t.Fatalf("lazy index: %v", err)
	}

	if len(got.Rules) != len(want.Rules) {
		t.Fatalf("lazy index: expected rules %v, got %v", want.Rules, got.Rules)
	}
	for i := range want.Rules {
		gotElse, wantElse := got.Else[got.Rules[i]], want.Else[want.Rules[i]]
		if !got.Rules[i].Equal(want.Rules[i]) || !slices.EqualFunc(gotElse, wantElse, (*Rule).Equal) {
			t.Fatalf("lazy index: expected rules %v (else %v), got %v (else %v)", want.Rules, want.Else, got.Rules, got.Else)
		}
	}
	if (got.Default == nil) != (want.Default == nil) || got.Default != nil && !got.Default.Equal(want.Default) {
		t.Fatalf("lazy index: expected default %v, got %v", want.Default, got.Default)
	}
	if got.Kind != want.Kind || got.EarlyExit != want.EarlyExit || got.OnlyGroundRefs != want.OnlyGroundRefs {
		t.Fatalf("lazy index: expected %v, early exit %v, only ground refs %v, got %v, %v, %v",
			want.Kind, want.EarlyExit, want.OnlyGroundRefs, got.Kind, got.EarlyExit, got.OnlyGroundRefs)
	}
}

func TestLazyIndexReads(t *testing.T) {
	module := MustParseModule(`package test

default f(_) := "default"

f(x) := "a" if x == "a"

f(x) := "b" if x == "b"
else := "b2" if x == "b2"
else := "b3"
`)
	for _, tc := range []struct {
		arg   string
		want  []string // rules and else branches, in order
		reads []int    // positions the lookup reads
	}{
		// The default rule is at position 0, so ids aren't positions. Every
		// lookup returns "b3", so position 2 is held rather than read.
		{arg: `"a"`, want: []string{`"a"`, `"b3"`}, reads: []int{1}},
		{arg: `"b2"`, want: []string{`"b2"`, `"b3"`}},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			src := &copySource{rules: module.Rules}
			index, err := newLazyIndex(func(Ref) bool { return false }, src)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(src.indexReads, []int{0, 1, 2}) || !slices.Equal(src.reads, []int{0, 2}) || index.defaultRule == nil {
				t.Fatalf("expected the build to read every rule once for the index, and keep the default and what every lookup returns, got %v and %v", src.indexReads, src.reads)
			}
			src.reads = nil

			resolver := testResolver{args: []Value{MustParseTerm(tc.arg).Value}}
			result, err := index.Lookup(resolver)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, rule := range result.Rules {
				got = append(got, rule.Head.Value.String())
				for _, branch := range result.Else[rule] {
					got = append(got, branch.Head.Value.String())
				}
			}
			if !slices.Equal(got, tc.want) || !slices.Equal(src.reads, tc.reads) {
				t.Errorf("expected %v reading %v, got %v reading %v", tc.want, tc.reads, got, src.reads)
			}

			eager := newBaseDocEqIndex(func(Ref) bool { return false })
			eager.Build(module.Rules)
			want, err := eager.Lookup(resolver)
			if err != nil {
				t.Fatal(err)
			}
			assertLazyAgrees(t, func(Ref) bool { return false }, module.Rules, false, resolver, want)
		})
	}
}

// A lazy index holds the rules a path without conditions reaches, unless an
// element in a collection decides; a rule that needs a ref defined is held too.
func TestLazyIndexHolds(t *testing.T) {
	module := MustParseModule(`package test

p if true

p if input.x

p if input.y == 1

p if data.groups.devs.members[input.subject]
`)
	index, err := newLazyIndex(func(Ref) bool { return false }, &copySource{rules: module.Rules})
	if err != nil {
		t.Fatal(err)
	}
	if held := slices.Sorted(maps.Keys(index.lazy.held)); !slices.Equal(held, []int32{0, 1}) {
		t.Errorf("expected to hold the rules at positions 0 and 1, got %v", held)
	}
}

func TestLazyIndexReadErrors(t *testing.T) {
	src := &copySource{rules: MustParseModule("package test\n\np if input.x == 1\np if input.x == 2\n").Rules}
	index, err := newLazyIndex(func(Ref) bool { return false }, src)
	if err != nil {
		t.Fatal(err)
	}
	src.err = errors.New("unreadable")
	if _, err := index.Lookup(testResolver{input: MustParseTerm(`{"x": 1}`)}); !errors.Is(err, src.err) {
		t.Errorf("expected Lookup to fail reading the rule, got %v", err)
	}
	if _, err := newLazyIndex(func(Ref) bool { return false }, src); !errors.Is(err, src.err) {
		t.Errorf("expected the build to fail reading a rule, got %v", err)
	}
}
