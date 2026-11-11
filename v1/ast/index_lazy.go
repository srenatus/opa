// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"errors"
	"slices"
)

// LazyRules supplies the compiled rules of a rule set by position, each with its
// else branches. Rule may be called concurrently. IndexRule gives a rule only to
// build the index from, which reads each rule once and keeps little of it: it
// may leave out locations and annotations. Experimental.
type LazyRules interface {
	Len() int
	Rule(i int) (*Rule, error)
	IndexRule(i int) (*Rule, error)
}

// lazyIndexRules is what a lazy index keeps of its rules: their heads, and where
// to read each rule from.
type lazyIndexRules struct {
	src    LazyRules
	heads  []*Head // distinct heads
	headOf []int32 // per id, into heads
	// depth is how many else branches below the rule at its position an id is,
	// nil if no id is one.
	depth []uint16
	// held are the rules, by position, that every lookup returns.
	held map[int32]*Rule
}

// newLazyIndex builds an index over the rules of src, reading each once. Lookups
// read the rules they return again.
func newLazyIndex(isVirtual func(Ref) bool, src LazyRules) (*baseDocEqIndex, error) {
	n := src.Len()
	if n == 0 {
		return nil, errors.New("rule index: no rules")
	}

	l := &lazyIndexRules{src: src, headOf: make([]int32, 0, n), depth: make([]uint16, 0, n)}
	i := newBaseDocEqIndex(isVirtual)

	// While building, a stub with the rule's head stands for each rule.
	headIDs := map[string]int32{}
	var depth uint16
	var root *Rule
	defaultAt := -1
	at := func(idx int) (*Rule, error) {
		r, err := src.IndexRule(idx)
		if err == nil && r.Default {
			defaultAt = idx
		}
		root, depth = r, 0
		return r, err
	}
	key := func(rule *Rule) *Rule {
		if rule != root {
			depth++
		}
		// Rules of a set mostly share their head, so the last one is tried first.
		h := int32(len(l.heads) - 1)
		if h < 0 || !sameHead(l.heads[h], rule.Head) {
			k := rule.Head.String()
			var ok bool
			if h, ok = headIDs[k]; !ok {
				h = int32(len(l.heads))
				headIDs[k] = h
				l.heads = append(l.heads, rule.Head)
			}
		}
		l.headOf = append(l.headOf, h)
		l.depth = append(l.depth, depth)
		return &Rule{Head: l.heads[h]}
	}

	if err := i.build(n, at, key); err != nil {
		return nil, err
	}
	// Lookups return the default rule as the index holds it, so it is read in full.
	if defaultAt >= 0 {
		r, err := src.Rule(defaultAt)
		if err != nil {
			return nil, err
		}
		i.defaultRule = r
	}
	// So are the rules a path without conditions reaches, unless a collection can
	// leave them out: they are what nearly every lookup returns.
	for _, id := range i.counting.always {
		pos := i.groups[id]
		if len(i.memberships[id]) > 0 || l.held[pos] != nil {
			continue
		}
		r, err := src.Rule(int(pos))
		if err != nil {
			return nil, err
		}
		if l.held == nil {
			l.held = map[int32]*Rule{}
		}
		l.held[pos] = r
	}
	i.rules, i.lazy = nil, l
	if !slices.ContainsFunc(l.depth, func(d uint16) bool { return d > 0 }) {
		l.depth = nil
	}
	return i, nil
}

// rule reads the rule or else branch with the given id from its position, as
// baseDocEqIndex.groups records it.
func (l *lazyIndexRules) rule(pos, id int32) (*Rule, error) {
	r, ok := l.held[pos]
	var err error
	if !ok {
		r, err = l.src.Rule(int(pos))
	}
	if err != nil || l.depth == nil {
		return r, err
	}
	for range l.depth[id] {
		r = r.Else
	}
	return r, nil
}

// sameHead is Head.Equal for heads that may lack a key or value, which
// Head.Compare doesn't take.
func sameHead(a, b *Head) bool {
	return (a.Key == nil) == (b.Key == nil) && (a.Value == nil) == (b.Value == nil) && a.Compare(b) == 0
}
