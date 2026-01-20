// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package sp

import (
	"context"
	"sync"

	"github.com/open-policy-agent/opa/v1/ast"
)

type CachedSource struct {
	underlying ast.ExternalRuleSource
	mu         sync.RWMutex
	cache      map[string][]*ast.Rule
}

func NewCachedSource(underlying ast.ExternalRuleSource) *CachedSource {
	return &CachedSource{
		underlying: underlying,
		cache:      make(map[string][]*ast.Rule),
	}
}

func (c *CachedSource) Refs() []ast.Ref {
	return c.underlying.Refs()
}

func (c *CachedSource) Init(ctx context.Context) (ast.ExternalRuleIndex, error) {
	index, err := c.underlying.Init(ctx)
	if err != nil {
		return nil, err
	}
	return &cachedIndex{
		underlying: index,
		cache:      c.cache,
		mu:         &c.mu,
	}, nil
}

type cachedIndex struct {
	underlying ast.ExternalRuleIndex
	cache      map[string][]*ast.Rule
	mu         *sync.RWMutex
}

func (c *cachedIndex) Lookup(ctx context.Context, input *ast.Term, _ ast.ValueResolver) ([]*ast.Rule, error) {
	return c.AllRules(ctx, input)
}

func (c *cachedIndex) AllRules(ctx context.Context, input *ast.Term) ([]*ast.Rule, error) {
	key := input.String()

	c.mu.RLock()
	if cached, ok := c.cache[key]; ok {
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	rules, err := c.underlying.AllRules(ctx, input)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache[key] = rules
	c.mu.Unlock()

	return rules, nil
}
