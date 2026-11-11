// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"fmt"
	"slices"

	"github.com/open-policy-agent/opa/v1/types"
	"github.com/open-policy-agent/opa/v1/util"
)

// LazyRuleSet is a rule set a Compiler keeps out of memory: it reads each rule
// once to index it, and again when an index lookup returns it. Summary must come
// from the compiler that compiled the rules, see Compiler.Summarize.
// Experimental.
type LazyRuleSet struct {
	Summary *RuleSetSummary
	Rules   LazyRules
}

// RuleSetSummary is what the rest of a compilation needs to know of a rule set
// whose rules it doesn't have. Experimental.
type RuleSetSummary struct {
	Path Ref
	// Head is the head of one of its rules: its kind, arity, and reference
	// relative to the package.
	Head *Head
	Type types.Type
	// Deps are the paths of the rule sets its rules depend on, DataRefs the base
	// documents they read, and Builtins the built-in functions they call.
	Deps     []Ref
	DataRefs []Ref
	Builtins []string
}

// WithLazyRuleSet adds a rule set the compiler keeps out of memory. No module may
// have rules at its path. Experimental.
func (c *Compiler) WithLazyRuleSet(set *LazyRuleSet) *Compiler {
	c.lazySets = append(c.lazySets, set)
	return c
}

// LazyRuleSets returns the compiler's lazy rule sets. Experimental.
func (c *Compiler) LazyRuleSets() []*LazyRuleSet {
	return c.lazySets
}

// LazyRuleSetOf returns the lazy rule set that rule is the placeholder of, nil if
// it is none. A placeholder stands in for a lazy rule set in the rule tree: it
// has the set's head, so that the rule tree tells there are rules at the set's
// path, but its body is no rule of the set's and fails when evaluated. Whatever
// reads rule bodies from the rule tree has to read the set's rules instead.
// Experimental.
func (c *Compiler) LazyRuleSetOf(rule *Rule) *LazyRuleSet {
	return c.lazyRules[rule]
}

// Summarize describes the compiled rule set at path for another compiler's
// LazyRuleSet. Its rules must all have path, a ground reference, and be in one
// package, with no rules above or below path. Experimental.
func (c *Compiler) Summarize(path Ref) (*RuleSetSummary, error) {
	node := c.RuleTree.Find(path)
	if node == nil || len(node.Values) == 0 {
		return nil, fmt.Errorf("no rules at %v", path)
	}
	if hasRulesBelow(node) {
		return nil, fmt.Errorf("rules below %v", path)
	}
	// A rule above with a general reference is indexed with everything below it.
	for i := 1; i < len(path); i++ {
		if above := c.RuleTree.Find(path[:i]); above != nil && len(above.Values) > 0 {
			return nil, fmt.Errorf("rules above %v", path)
		}
	}

	deps, data, builtins := map[string]Ref{}, map[string]Ref{}, map[string]struct{}{}
	addBuiltin := func(op Ref) {
		if name := op.String(); c.builtins[name] != nil {
			builtins[name] = struct{}{}
		}
	}
	pkg := node.Values[0].Module.Package.Path
	for _, r := range node.Values {
		if ref := r.Ref(); !ref.IsGround() || !ref.Equal(path) {
			return nil, fmt.Errorf("rule %v doesn't have the reference %v", ref, path)
		}
		if !r.Module.Package.Path.Equal(pkg) {
			return nil, fmt.Errorf("rules of %v in packages %v and %v", path, pkg, r.Module.Package.Path)
		}
		WalkRules(r, func(rule *Rule) bool {
			for dep := range c.Graph.Dependencies(rule) {
				ref := dep.(*Rule).Ref().GroundPrefix()
				deps[ref.String()] = ref
			}
			WalkTerms(rule, func(t *Term) bool {
				switch v := t.Value.(type) {
				case Ref:
					if v.HasPrefix(DefaultRootRef) && !c.isVirtual(v) {
						ref := v.GroundPrefix()
						data[ref.String()] = ref
					}
				case Call:
					if op, ok := v[0].Value.(Ref); ok {
						addBuiltin(op)
					}
				}
				return false
			})
			WalkExprs(rule, func(e *Expr) bool {
				if e.IsCall() {
					addBuiltin(e.Operator())
				}
				return false
			})
			return false
		})
	}

	s := &RuleSetSummary{
		Path:     path,
		Head:     node.Values[0].Head.Copy(),
		Type:     c.TypeEnv.GetByRef(path),
		Deps:     sortedRefs(deps),
		DataRefs: sortedRefs(data),
	}
	for name := range builtins {
		s.Builtins = append(s.Builtins, name)
	}
	slices.Sort(s.Builtins)
	return s, nil
}

// pkg is the package of the rule set's rules.
func (s *RuleSetSummary) pkg() Ref {
	return s.Path[:len(s.Path)-len(s.Head.Ref())]
}

func hasRulesBelow(node *TreeNode) bool {
	for _, child := range node.Children {
		if len(child.Values) > 0 || hasRulesBelow(child) {
			return true
		}
	}
	return false
}

func sortedRefs(refs map[string]Ref) []Ref {
	result := make([]Ref, 0, len(refs))
	for _, ref := range refs {
		result = append(result, ref)
	}
	slices.SortFunc(result, func(a, b Ref) int { return a.Compare(b) })
	return result
}

// lazyRuleSetPlaceholderOp is called by a placeholder's body. It is no built-in
// function, so evaluating or planning the body fails rather than taking it for
// the set's rules.
var lazyRuleSetPlaceholderOp = RefTerm(VarTerm("internal"), StringTerm("lazy_rule_set_placeholder"))

// addLazySets puts a placeholder rule for each lazy rule set into the rule tree,
// which buildRuleIndices replaces with the set's index. It is never evaluated.
func (c *Compiler) addLazySets() {
	if len(c.lazySets) == 0 {
		return
	}
	c.lazyRules = make(map[*Rule]*LazyRuleSet, len(c.lazySets))
	for _, set := range c.lazySets {
		s := set.Summary
		if node := c.RuleTree.Find(s.Path); node != nil && len(node.Values) > 0 {
			c.err(NewError(CompileErr, node.Values[0].Loc(), "rules at %v are also given as a lazy rule set", s.Path))
			continue
		}
		placeholder := &Rule{
			Head:   s.Head.Copy(),
			Body:   NewBody(NewExpr([]*Term{lazyRuleSetPlaceholderOp, StringTerm(s.Path.String())})),
			Module: &Module{Package: &Package{Path: s.pkg()}},
		}
		c.RuleTree.add(s.Path, placeholder)
		if !c.enablePrintStatements && slices.Contains(s.Builtins, InternalPrint.Name) {
			// Rule sets are compiled with print statements kept.
			set = &LazyRuleSet{Summary: s, Rules: printErased{set.Rules}}
		}
		c.lazyRules[placeholder] = set
	}
	c.RuleTree.DepthFirst(func(x *TreeNode) bool {
		slices.SortFunc(x.Sorted, Value.Compare)
		return false
	})
}

// addLazyDependencies adds the lazy rule sets' dependencies to the graph: the
// rule sets they were compiled against, which must still have rules, and the
// rules at the base documents they read, if any have rules now.
func (c *Compiler) addLazyDependencies() {
	list := func(r Ref) []*Rule {
		return c.GetRulesDynamicWithOpts(r, RulesOptions{IncludeHiddenModules: true})
	}
	for placeholder, set := range c.lazyRules {
		c.Graph.addNode(placeholder)
		depend := func(rules []*Rule) {
			for _, rule := range rules {
				for node := rule; node != nil; node = node.Else {
					c.Graph.addDependency(placeholder, node)
				}
			}
		}
		for _, dep := range set.Summary.Deps {
			rules := list(dep)
			if len(rules) == 0 {
				c.err(NewError(CompileErr, nil, "lazy rule set %v depends on %v, which has no rules", set.Summary.Path, dep))
			}
			depend(rules)
		}
		// Evaluation reads these from rules now, which the recursion check has to
		// see the set depend on.
		for _, ref := range set.Summary.DataRefs {
			depend(list(ref))
		}
	}
	c.Graph.sorted = nil
}

// seedLazyTypes returns an environment with the lazy rule sets' types for the
// type checker, and the rules it checks: all but the placeholders.
func (c *Compiler) seedLazyTypes(sorted []util.T) (*TypeEnv, []util.T) {
	if len(c.lazyRules) == 0 {
		return c.TypeEnv, sorted
	}
	// Wrapped, as the checker does for the rules it checks: refs it can't place in
	// the innermost environment are looked for in every document there.
	env := c.TypeEnv.wrap()
	for _, set := range c.lazyRules {
		env.tree.Put(set.Summary.Path, set.Summary.Type)
	}
	return env, slices.DeleteFunc(slices.Clone(sorted), func(x util.T) bool {
		_, ok := c.lazyRules[x.(*Rule)]
		return ok
	})
}

// checkLazyBuiltins requires the built-in functions the lazy rule sets call, and
// checks they are available and allowed.
func (c *Compiler) checkLazyBuiltins() {
	for _, set := range c.lazyRules {
		for _, name := range set.Summary.Builtins {
			bi := c.builtins[name]
			if bi == nil {
				c.err(NewError(CompileErr, nil, "lazy rule set %v calls %v, which is not a built-in function", set.Summary.Path, name))
				continue
			}
			if _, unsafe := c.unsafeBuiltinsMap[name]; unsafe {
				c.err(NewError(CompileErr, nil, "lazy rule set %v calls unsafe built-in function %v", set.Summary.Path, name))
				continue
			}
			c.Required.addBuiltinSorted(bi)
		}
	}
}

// lazyIndex builds the index of a lazy rule set, if rules is its placeholder.
func (c *Compiler) lazyIndex(rules []*Rule) (RuleIndex, bool) {
	if len(rules) != 1 {
		return nil, false
	}
	set, ok := c.lazyRules[rules[0]]
	if !ok {
		return nil, false
	}
	index, err := newLazyIndex(c.isVirtual, set.Rules)
	if err != nil {
		c.err(NewError(CompileErr, nil, "lazy rule set %v: %v", set.Summary.Path, err))
		return nil, true
	}
	return index, true
}

// printErased reads rules with their print calls erased.
type printErased struct{ LazyRules }

func (p printErased) Rule(i int) (*Rule, error) {
	return erased(p.LazyRules.Rule(i))
}

func (p printErased) IndexRule(i int) (*Rule, error) {
	return erased(p.LazyRules.IndexRule(i))
}

func erased(r *Rule, err error) (*Rule, error) {
	if err != nil {
		return nil, err
	}
	NewGenericVisitor(func(x any) bool {
		switch x := x.(type) {
		case *Rule:
			x.Body = eraseCompiledPrintCalls(x.Body)
		case *ArrayComprehension:
			x.Body = eraseCompiledPrintCalls(x.Body)
		case *SetComprehension:
			x.Body = eraseCompiledPrintCalls(x.Body)
		case *ObjectComprehension:
			x.Body = eraseCompiledPrintCalls(x.Body)
		case *Every:
			x.Body = eraseCompiledPrintCalls(x.Body)
		}
		return false
	}).Walk(r)
	return r, nil
}

// eraseCompiledPrintCalls erases the internal.print calls the compiler rewrote
// print calls into, and the comprehensions computing their operands: the body is
// as if the print calls had been erased before compiling.
func eraseCompiledPrintCalls(body Body) Body {
	operands := NewVarSet()
	found := false
	for _, e := range body {
		if e.IsCall() && e.Operator().Equal(Interned.Refs.InternalPrint) {
			found = true
			if arr, ok := e.Operand(0).Value.(*Array); ok {
				arr.Foreach(func(t *Term) {
					if v, ok := t.Value.(Var); ok {
						operands.Add(v)
					}
				})
			}
		}
	}
	if !found {
		return body
	}
	out := make(Body, 0, len(body))
	for _, e := range body {
		if e.IsCall() && e.Operator().Equal(Interned.Refs.InternalPrint) {
			continue
		}
		if e.IsEquality() {
			if v, ok := e.Operand(0).Value.(Var); ok && operands.Contains(v) {
				if _, ok := e.Operand(1).Value.(*SetComprehension); ok {
					continue
				}
			}
		}
		out.Append(e)
	}
	if len(out) == 0 {
		out.Append(NewExpr(BooleanTerm(true).SetLocation(body.Loc())).SetLocation(body.Loc()))
	}
	return out
}
