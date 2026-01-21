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

func (s *StaticSource) Init(context.Context, ast.Ref) (ast.ExternalRuleIndex, error) {
	return &staticIndex{rules: s.rules}, nil
}

type staticIndex struct {
	rules []*ast.Rule
}

func (s *staticIndex) Lookup(context.Context, ast.ValueResolver) (*ast.IndexResult, error) {
	return &ast.IndexResult{Rules: s.rules}, nil
}

func (s *staticIndex) AllRules(context.Context, ast.ValueResolver) (*ast.IndexResult, error) {
	return &ast.IndexResult{Rules: s.rules}, nil
}

var (
	_ ast.ExternalRuleSource = (*StaticSource)(nil)
)
