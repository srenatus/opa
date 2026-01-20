// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package sp

import (
	"fmt"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
)

func TestStaticSource(t *testing.T) {
	rules := []*ast.Rule{
		ast.MustParseRule(`p { true }`),
		ast.MustParseRule(`q { true }`),
	}

	refs := []ast.Ref{ast.MustParseRef("data.test")}
	source := NewStaticSource(refs, rules)

	ctx := t.Context()
	input := ast.NullTerm()

	index, err := source.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	result, err := index.AllRules(ctx, input)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}

	if len(result) != len(rules) {
		t.Errorf("Expected %d rules, got %d", len(rules), len(result))
	}

	mockResolver := &mockResolver{}
	result2, err := index.Lookup(ctx, input, mockResolver)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if len(result2) != len(rules) {
		t.Errorf("Expected %d rules from Lookup, got %d", len(rules), len(result2))
	}
}

func TestFilteredSource(t *testing.T) {
	rules := []*ast.Rule{
		ast.MustParseRule(`p { true }`),
		ast.MustParseRule(`q { true }`),
		ast.MustParseRule(`r { true }`),
	}

	filter := func(_ *ast.Term, rule *ast.Rule) bool {
		name := string(rule.Head.Name)
		return name == "p" || name == "q"
	}

	refs := []ast.Ref{ast.MustParseRef("data.test")}
	source := NewFilteredSource(refs, rules, filter)

	ctx := t.Context()
	input := ast.NullTerm()

	index, err := source.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	result, err := index.AllRules(ctx, input)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 rules after filtering, got %d", len(result))
	}

	for _, rule := range result {
		name := string(rule.Head.Name)
		if name != "p" && name != "q" {
			t.Errorf("Unexpected rule %s in filtered results", name)
		}
	}

	mockResolver := &mockResolver{}
	result2, err := index.Lookup(ctx, input, mockResolver)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if len(result2) != len(result) {
		t.Errorf("Expected Lookup to return same as AllRules")
	}
}

func TestFilteredSource_InputBasedFiltering(t *testing.T) {
	rules := []*ast.Rule{
		ast.MustParseRule(`allow { true }`),
		ast.MustParseRule(`deny { true }`),
	}

	filter := func(input *ast.Term, rule *ast.Rule) bool {
		if obj, ok := input.Value.(ast.Object); ok {
			if mode := obj.Get(ast.StringTerm("mode")); mode != nil {
				if str, ok := mode.Value.(ast.String); ok {
					ruleName := string(rule.Head.Name)
					return ruleName == string(str)
				}
			}
		}
		return true
	}

	refs := []ast.Ref{ast.MustParseRef("data.test")}
	source := NewFilteredSource(refs, rules, filter)
	ctx := t.Context()

	index, err := source.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	inputAllow := ast.ObjectTerm([2]*ast.Term{ast.StringTerm("mode"), ast.StringTerm("allow")})
	result, err := index.AllRules(ctx, inputAllow)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(result))
	}

	if len(result) > 0 && string(result[0].Head.Name) != "allow" {
		t.Errorf("Expected 'allow' rule, got '%s'", result[0].Head.Name)
	}

	inputDeny := ast.ObjectTerm([2]*ast.Term{ast.StringTerm("mode"), ast.StringTerm("deny")})
	result2, err := index.AllRules(ctx, inputDeny)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}

	if len(result2) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(result2))
	}

	if len(result2) > 0 && string(result2[0].Head.Name) != "deny" {
		t.Errorf("Expected 'deny' rule, got '%s'", result2[0].Head.Name)
	}
}

func TestIndexedSource(t *testing.T) {
	rules := []*ast.Rule{
		ast.MustParseRule(`p { true }`),
		ast.MustParseRule(`q { true }`),
	}

	lookup := func(_ *ast.Term, resolver ast.ValueResolver, rule *ast.Rule) bool {
		return true
	}

	refs := []ast.Ref{ast.MustParseRef("data.test")}
	source := NewIndexedSource(refs, rules, lookup)

	ctx := t.Context()
	input := ast.NullTerm()

	index, err := source.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	result, err := index.AllRules(ctx, input)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}

	if len(result) != len(rules) {
		t.Errorf("Expected %d rules, got %d", len(rules), len(result))
	}

	mockResolver := &mockResolver{}
	result2, err := index.Lookup(ctx, input, mockResolver)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if len(result2) != len(rules) {
		t.Errorf("Expected %d rules from Lookup, got %d", len(rules), len(result2))
	}
}

func TestIndexedSource_SelectiveFiltering(t *testing.T) {
	rules := []*ast.Rule{
		ast.MustParseRule(`p { true }`),
		ast.MustParseRule(`q { true }`),
		ast.MustParseRule(`r { true }`),
	}

	lookup := func(_ *ast.Term, resolver ast.ValueResolver, rule *ast.Rule) bool {
		name := string(rule.Head.Name)
		return len(name) == 1
	}

	refs := []ast.Ref{ast.MustParseRef("data.test")}
	source := NewIndexedSource(refs, rules, lookup)
	ctx := t.Context()
	input := ast.NullTerm()

	index, err := source.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	result, err := index.AllRules(ctx, input)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 rules from AllRules, got %d", len(result))
	}

	mockResolver := &mockResolver{}
	result2, err := index.Lookup(ctx, input, mockResolver)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if len(result2) != 3 {
		t.Errorf("Expected 3 rules (all have single-char names), got %d", len(result2))
	}
}

func TestErrorSource(t *testing.T) {
	expectedErr := fmt.Errorf("test error")
	refs := []ast.Ref{ast.MustParseRef("data.test")}
	source := NewErrorSource(refs, expectedErr)

	ctx := t.Context()

	index, err := source.Init(ctx)
	if err == nil {
		t.Fatal("Expected error from Init, got nil")
	}

	if err.Error() != expectedErr.Error() {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}

	if index != nil {
		t.Error("Expected nil index when Init returns error")
	}

	_, err = source.Init(ctx)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if err.Error() != expectedErr.Error() {
		t.Errorf("Expected error %v, got %v", expectedErr, err)
	}
}

func TestErrorSource_NilError(t *testing.T) {
	refs := []ast.Ref{ast.MustParseRef("data.test")}
	source := NewErrorSource(refs, nil)

	ctx := t.Context()
	_, err := source.Init(ctx)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
}

type mockResolver struct{}

func (m *mockResolver) Resolve(ref ast.Ref) (ast.Value, error) {
	return nil, fmt.Errorf("not implemented")
}
