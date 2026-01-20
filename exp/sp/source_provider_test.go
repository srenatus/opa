// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package sp

import (
	"context"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
)

func TestMockSource(t *testing.T) {
	rules := []*ast.Rule{
		ast.MustParseRule(`p { true }`),
		ast.MustParseRule(`q { true }`),
	}

	refs := []ast.Ref{ast.MustParseRef("data.test")}
	source := &mockSource{refs: refs, rules: rules}

	ctx := t.Context()
	input := ast.NullTerm()
	ref := ast.MustParseRef("data.test")
	mockResolver := &mockResolver{}

	index, err := source.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	result, err := index.Lookup(ctx, ref, input, mockResolver)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 rules from Lookup, got %d", len(result))
	}

	result2, err := index.AllRules(ctx, ref, input)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}

	if len(result2) != 2 {
		t.Errorf("Expected 2 rules from AllRules, got %d", len(result2))
	}
}

type mockSource struct {
	refs  []ast.Ref
	rules []*ast.Rule
}

func (m *mockSource) Refs() []ast.Ref {
	return m.refs
}

func (m *mockSource) Init(context.Context) (ast.ExternalRuleIndex, error) {
	return &mockIndex{rules: m.rules}, nil
}

type mockIndex struct {
	rules []*ast.Rule
}

func (m *mockIndex) Lookup(context.Context, ast.Ref, *ast.Term, ast.ValueResolver) ([]*ast.Rule, error) {
	return m.rules, nil
}

func (m *mockIndex) AllRules(context.Context, ast.Ref, *ast.Term) ([]*ast.Rule, error) {
	return m.rules, nil
}
