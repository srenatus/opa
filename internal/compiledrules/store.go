// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package compiledrules

import (
	"bytes"
	"context"
	"slices"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/util"
)

func parseModule(path string, raw []byte, opts ast.ParserOptions) (*ast.Module, error) {
	return ast.ParseModuleWithOpts(path, util.ByteSliceToString(raw), opts)
}

// PoliciesFromStore parses the store's policies with popts, leaving out the
// rules of the compiled rule sets the store keeps (see storage.CompiledRules),
// which it returns for the compiler; see ast.Compiler.WithLazyRuleSet. If the
// policies don't hold exactly those rules, it parses them in full and returns
// no rule sets.
func PoliciesFromStore(ctx context.Context, store storage.Store, txn storage.Transaction, popts ast.ParserOptions) (map[string]*ast.Module, []*ast.LazyRuleSet, error) {
	ids, err := store.ListPolicies(ctx, txn)
	if err != nil {
		return nil, nil, err
	}
	raw := make(map[string][]byte, len(ids))
	for _, id := range ids {
		if raw[id], err = store.GetPolicy(ctx, txn, id); err != nil {
			return nil, nil, err
		}
	}

	parseAll := func(parse ParseFunc) (map[string]*ast.Module, error) {
		modules := make(map[string]*ast.Module, len(raw))
		for id, bs := range raw {
			m, err := parse(id, bs, popts)
			if err != nil {
				return nil, err
			}
			modules[id] = m
		}
		return modules, nil
	}

	if use := FromStore(ctx, store, txn, nil); use != nil {
		modules, err := parseAll(use.Parser(parseModule))
		if err == nil && use.Complete() {
			return modules, use.Sets, nil
		}
	}
	modules, err := parseAll(parseModule)
	return modules, nil, err
}

// ModuleWithLazyRules returns m with the rules of sets that raw, its source,
// holds put back in, decoded, and every rule in source order; m itself if raw
// holds none.
func ModuleWithLazyRules(m *ast.Module, raw []byte, sets []*ast.LazyRuleSet) (*ast.Module, error) {
	type ruleAt struct {
		set *ast.LazyRuleSet
		i   int
	}
	byKey := map[ast.RuleKey][]ruleAt{}
	for _, s := range sets {
		for i, k := range Keys(s) {
			byKey[k] = append(byKey[k], ruleAt{s, i})
		}
	}
	if m == nil || len(byKey) == 0 {
		return m, nil
	}

	type placed struct {
		rule *ast.Rule
		pos  int
	}
	// Positions are rows: parsing without the lazy rules keeps rows, not offsets.
	var lazy []placed
	rows := map[ast.RuleKey][]int{}
	row, off := 1, 0
	for s, key := range KeyedStatements(raw, true) {
		row, off = row+bytes.Count(raw[off:s.start], []byte("\n")), s.start
		if at := byKey[key]; len(at) > 0 {
			r, err := at[0].set.Rules.Rule(at[0].i)
			if err != nil {
				return nil, err
			}
			byKey[key] = at[1:]
			lazy = append(lazy, placed{r, row})
			continue
		}
		rows[key] = append(rows[key], row)
	}
	if len(lazy) == 0 {
		return m, nil
	}

	rules := make([]placed, 0, len(m.Rules)+len(lazy))
	for i, r := range m.Rules {
		pos := row + 1 + i
		if r.Location != nil {
			pos = r.Location.Row
		} else if k, ok := r.SourceKey(); ok && len(rows[k]) > 0 {
			pos, rows[k] = rows[k][0], rows[k][1:]
		}
		rules = append(rules, placed{r, pos})
	}
	rules = append(rules, lazy...)
	slices.SortStableFunc(rules, func(a, b placed) int { return a.pos - b.pos })

	cpy := *m
	cpy.Rules = make([]*ast.Rule, len(rules))
	onRules := map[*ast.Annotations]struct{}{}
	for i, p := range rules {
		cpy.Rules[i] = p.rule
		for _, a := range p.rule.Annotations {
			onRules[a] = struct{}{}
		}
	}
	// The module's own annotations, then those of its rules in their order.
	cpy.Annotations = slices.DeleteFunc(slices.Clone(m.Annotations), func(a *ast.Annotations) bool {
		_, ok := onRules[a]
		return ok
	})
	seen := map[*ast.Annotations]struct{}{}
	for _, r := range cpy.Rules {
		for _, a := range r.Annotations {
			if _, ok := seen[a]; !ok {
				seen[a] = struct{}{}
				cpy.Annotations = append(cpy.Annotations, a)
			}
		}
	}
	return &cpy, nil
}
