// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package sp

import (
	"context"

	"github.com/open-policy-agent/opa/v1/ast"
)

type StaticSource struct {
	refs  []ast.Ref
	rules []*ast.Rule
}

func NewStaticSource(refs []ast.Ref, rules []*ast.Rule) *StaticSource {
	return &StaticSource{refs: refs, rules: rules}
}

func (s *StaticSource) Refs() []ast.Ref {
	return s.refs
}

func (s *StaticSource) Init(_ context.Context, r ast.Ref) (ast.ExternalRuleIndex, error) {
	rules := []*ast.Rule{}
	for i := range s.rules {
		rules = append(rules, s.rules[i].Copy())
	}
	return &staticIndex{rules: rules}, nil
}

type staticIndex struct {
	rules []*ast.Rule
}

func (s *staticIndex) Lookup(_ context.Context, res ast.ValueResolver) ([]*ast.Rule, error) {
	inp, _ := res.Resolve(ast.InputRootRef)
	_ = inp
	rules := make([]*ast.Rule, len(s.rules))
	for i := range s.rules {
		rules[i] = s.rules[i].Copy()
	}
	return rules, nil
}

func (s *staticIndex) AllRules(context.Context, ast.ValueResolver) ([]*ast.Rule, error) {
	return s.rules, nil
}

var (
	_ ast.ExternalRuleSource = (*StaticSource)(nil)
)
