// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package sp

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
)

type countingSource struct {
	rules     []*ast.Rule
	callCount int32
}

func (c *countingSource) Init(context.Context) (ast.ExternalRuleIndex, error) {
	return &countingIndex{rules: c.rules, callCount: &c.callCount}, nil
}

type countingIndex struct {
	rules     []*ast.Rule
	callCount *int32
}

func (c *countingIndex) Lookup(context.Context, *ast.Term, ast.ValueResolver) ([]*ast.Rule, error) {
	atomic.AddInt32(c.callCount, 1)
	return c.rules, nil
}

func (c *countingIndex) AllRules(context.Context, *ast.Term) ([]*ast.Rule, error) {
	atomic.AddInt32(c.callCount, 1)
	return c.rules, nil
}

func (c *countingSource) getCallCount() int {
	return int(atomic.LoadInt32(&c.callCount))
}

func TestCachedSource_AllRules(t *testing.T) {
	ctx := t.Context()

	// Parse a simple rule
	module := ast.MustParseModule(`package test
p if true`)

	underlying := &countingSource{rules: module.Rules}
	cached := NewCachedSource(underlying)

	index, err := cached.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	input1 := ast.MustParseTerm(`{"user": "alice"}`)
	input2 := ast.MustParseTerm(`{"user": "bob"}`)

	// First call with input1 - should hit underlying source
	rules1, err := index.AllRules(ctx, input1)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}
	if len(rules1) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules1))
	}
	if underlying.getCallCount() != 1 {
		t.Errorf("Expected 1 call to underlying after first AllRules, got %d", underlying.getCallCount())
	}

	// Second call with same input1 - should use cache
	rules2, err := index.AllRules(ctx, input1)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}
	if len(rules2) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules2))
	}
	if underlying.getCallCount() != 1 {
		t.Errorf("Expected 1 call to underlying after cache hit, got %d", underlying.getCallCount())
	}

	// Third call with different input2 - should hit underlying source
	rules3, err := index.AllRules(ctx, input2)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}
	if len(rules3) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules3))
	}
	if underlying.getCallCount() != 2 {
		t.Errorf("Expected 2 calls to underlying after new input, got %d", underlying.getCallCount())
	}

	// Fourth call with input2 again - should use cache
	rules4, err := index.AllRules(ctx, input2)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}
	if len(rules4) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules4))
	}
	if underlying.getCallCount() != 2 {
		t.Errorf("Expected 2 calls to underlying after cache hit, got %d", underlying.getCallCount())
	}
}

func TestCachedSource_Lookup(t *testing.T) {
	ctx := t.Context()

	module := ast.MustParseModule(`package test
p if true`)

	underlying := &countingSource{rules: module.Rules}
	cached := NewCachedSource(underlying)

	index, err := cached.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	input := ast.MustParseTerm(`{"user": "alice"}`)

	// First call - should hit underlying
	rules1, err := index.Lookup(ctx, input, nil)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if len(rules1) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules1))
	}
	if underlying.getCallCount() != 1 {
		t.Errorf("Expected 1 call to underlying, got %d", underlying.getCallCount())
	}

	// Second call - should use cache
	rules2, err := index.Lookup(ctx, input, nil)
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if len(rules2) != 1 {
		t.Fatalf("Expected 1 rule, got %d", len(rules2))
	}
	if underlying.getCallCount() != 1 {
		t.Errorf("Expected 1 call to underlying (cache hit), got %d", underlying.getCallCount())
	}
}
