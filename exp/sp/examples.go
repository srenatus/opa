// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package sp

import (
	"context"
	"fmt"

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

func (s *StaticSource) Init(context.Context) (ast.ExternalRuleIndex, error) {
	return &staticIndex{rules: s.rules}, nil
}

type staticIndex struct {
	rules []*ast.Rule
}

func (s *staticIndex) Lookup(context.Context, *ast.Term, ast.ValueResolver) ([]*ast.Rule, error) {
	return s.rules, nil
}

func (s *staticIndex) AllRules(context.Context, *ast.Term) ([]*ast.Rule, error) {
	return s.rules, nil
}

type FilteredSource struct {
	refs     []ast.Ref
	allRules []*ast.Rule
	filter   func(*ast.Term, *ast.Rule) bool
}

func NewFilteredSource(refs []ast.Ref, rules []*ast.Rule, filter func(*ast.Term, *ast.Rule) bool) *FilteredSource {
	return &FilteredSource{
		refs:     refs,
		allRules: rules,
		filter:   filter,
	}
}

func (s *FilteredSource) Refs() []ast.Ref {
	return s.refs
}

func (s *FilteredSource) Init(context.Context) (ast.ExternalRuleIndex, error) {
	return &filteredIndex{allRules: s.allRules, filter: s.filter}, nil
}

type filteredIndex struct {
	allRules []*ast.Rule
	filter   func(*ast.Term, *ast.Rule) bool
}

func (s *filteredIndex) Lookup(ctx context.Context, input *ast.Term, _ ast.ValueResolver) ([]*ast.Rule, error) {
	return s.AllRules(ctx, input)
}

func (s *filteredIndex) AllRules(_ context.Context, input *ast.Term) ([]*ast.Rule, error) {
	if s.filter == nil {
		return s.allRules, nil
	}

	filtered := make([]*ast.Rule, 0, len(s.allRules))
	for _, rule := range s.allRules {
		if s.filter(input, rule) {
			filtered = append(filtered, rule)
		}
	}
	return filtered, nil
}

type IndexedSource struct {
	refs     []ast.Ref
	allRules []*ast.Rule
	lookup   func(*ast.Term, ast.ValueResolver, *ast.Rule) bool
}

func NewIndexedSource(refs []ast.Ref, rules []*ast.Rule, lookup func(*ast.Term, ast.ValueResolver, *ast.Rule) bool) *IndexedSource {
	return &IndexedSource{
		refs:     refs,
		allRules: rules,
		lookup:   lookup,
	}
}

func (s *IndexedSource) Refs() []ast.Ref {
	return s.refs
}

func (s *IndexedSource) Init(context.Context) (ast.ExternalRuleIndex, error) {
	return &indexedIndex{allRules: s.allRules, lookup: s.lookup}, nil
}

type indexedIndex struct {
	allRules []*ast.Rule
	lookup   func(*ast.Term, ast.ValueResolver, *ast.Rule) bool
}

func (s *indexedIndex) Lookup(_ context.Context, input *ast.Term, resolver ast.ValueResolver) ([]*ast.Rule, error) {
	if s.lookup == nil {
		return s.allRules, nil
	}

	filtered := make([]*ast.Rule, 0, len(s.allRules))
	for _, rule := range s.allRules {
		if s.lookup(input, resolver, rule) {
			filtered = append(filtered, rule)
		}
	}
	return filtered, nil
}

func (s *indexedIndex) AllRules(context.Context, *ast.Term) ([]*ast.Rule, error) {
	return s.allRules, nil
}

type ErrorSource struct {
	refs []ast.Ref
	err  error
}

func NewErrorSource(refs []ast.Ref, err error) *ErrorSource {
	if err == nil {
		err = fmt.Errorf("error source")
	}
	return &ErrorSource{refs: refs, err: err}
}

func (s *ErrorSource) Refs() []ast.Ref {
	return s.refs
}

func (s *ErrorSource) Init(context.Context) (ast.ExternalRuleIndex, error) {
	return nil, s.err
}

var (
	_ ast.ExternalRuleSource = (*StaticSource)(nil)
	_ ast.ExternalRuleSource = (*FilteredSource)(nil)
	_ ast.ExternalRuleSource = (*IndexedSource)(nil)
	_ ast.ExternalRuleSource = (*ErrorSource)(nil)
)
