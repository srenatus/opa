package ast

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

type mockExternalSource struct {
	refs      []Ref
	rules     []*Rule
	callCount int32
	mu        sync.Mutex
}

func newMockExternalSource(refs []Ref, rules []*Rule) *mockExternalSource {
	return &mockExternalSource{
		refs:      refs,
		rules:     rules,
		callCount: 0,
	}
}

func (m *mockExternalSource) Refs() []Ref {
	return m.refs
}

func (m *mockExternalSource) Init(context.Context) (ExternalRuleIndex, error) {
	return &mockExternalIndex{rules: m.rules, callCount: &m.callCount}, nil
}

type mockExternalIndex struct {
	rules     []*Rule
	callCount *int32
}

func (m *mockExternalIndex) Lookup(ctx context.Context, ref Ref, input *Term, resolver ValueResolver) ([]*Rule, error) {
	atomic.AddInt32(m.callCount, 1)
	return m.rules, nil
}

func (m *mockExternalIndex) AllRules(ctx context.Context, ref Ref, input *Term) ([]*Rule, error) {
	atomic.AddInt32(m.callCount, 1)
	return m.rules, nil
}

func (m *mockExternalSource) getCallCount() int {
	return int(atomic.LoadInt32(&m.callCount))
}

func TestCompilerRuleIndexReturnsNilForExternalSources(t *testing.T) {
	rule := &Rule{
		Head: &Head{
			Reference: MustParseRef("data.external.test.foo"),
			Value:     BooleanTerm(true),
		},
		Body: NewBody(
			Equality.Expr(VarTerm("x"), IntNumberTerm(1)),
		),
	}

	packageRef := MustParseRef("data.external.test")
	source := newMockExternalSource([]Ref{packageRef}, []*Rule{rule})
	compiler := NewCompiler()
	compiler.WithExternalSource(packageRef, source)

	index := compiler.RuleIndex(packageRef)
	if index != nil {
		t.Error("Expected RuleIndex to return nil for external source path (delegation to evaluation-time)")
	}

	if source.getCallCount() != 0 {
		t.Errorf("Expected GetRules NOT to be called at compile-time, got %d calls", source.getCallCount())
	}
}

func TestExternalSourcePrefixMatching(t *testing.T) {
	rule := &Rule{
		Head: &Head{
			Reference: MustParseRef("data.external.pkg.foo"),
			Value:     BooleanTerm(true),
		},
		Body: NewBody(Equality.Expr(VarTerm("x"), IntNumberTerm(1))),
	}

	packageRef := MustParseRef("data.external.pkg")
	source := newMockExternalSource([]Ref{packageRef}, []*Rule{rule})

	compiler := NewCompiler()
	compiler.WithExternalSource(packageRef, source)

	findSource := func(query Ref) ExternalRuleSource {
		var found ExternalRuleSource
		compiler.IterateExternalSources(func(pkgRef Ref, src ExternalRuleSource) bool {
			if query.HasPrefix(pkgRef) {
				found = src
				return true
			}
			return false
		})
		return found
	}

	found := findSource(packageRef)
	if found != source {
		t.Error("Expected to find source for exact package match")
	}

	subPath := MustParseRef("data.external.pkg.foo")
	found = findSource(subPath)
	if found != source {
		t.Error("Expected to find source for sub-path (prefix match)")
	}

	otherPath := MustParseRef("data.other.pkg")
	found = findSource(otherPath)
	if found != nil {
		t.Error("Expected nil for non-matching path")
	}
}

func TestIterateExternalSources(t *testing.T) {
	rule1 := &Rule{
		Head: &Head{
			Reference: MustParseRef("data.pkg1.foo"),
			Value:     IntNumberTerm(1),
		},
		Body: NewBody(NewExpr(BooleanTerm(true))),
	}

	rule2 := &Rule{
		Head: &Head{
			Reference: MustParseRef("data.pkg2.bar"),
			Value:     IntNumberTerm(2),
		},
		Body: NewBody(NewExpr(BooleanTerm(true))),
	}

	source1 := newMockExternalSource([]Ref{MustParseRef("data.pkg1")}, []*Rule{rule1})
	source2 := newMockExternalSource([]Ref{MustParseRef("data.pkg2")}, []*Rule{rule2})

	compiler := NewCompiler()
	compiler.WithExternalSource(MustParseRef("data.pkg1"), source1)
	compiler.WithExternalSource(MustParseRef("data.pkg2"), source2)

	foundSources := make(map[string]ExternalRuleSource)
	compiler.IterateExternalSources(func(ref Ref, src ExternalRuleSource) bool {
		foundSources[ref.String()] = src
		return false
	})

	if len(foundSources) != 2 {
		t.Errorf("Expected 2 sources, got %d", len(foundSources))
	}

	if foundSources["data.pkg1"] != source1 {
		t.Error("Expected to find source1 at data.pkg1")
	}

	if foundSources["data.pkg2"] != source2 {
		t.Error("Expected to find source2 at data.pkg2")
	}
}

func TestExternalSourceInputParameter(t *testing.T) {
	var receivedInput *Term

	packageRef := MustParseRef("data.capture")
	captureSource := &inputCapturingSource{
		refs: []Ref{packageRef},
		rules: []*Rule{
			{
				Head: &Head{
					Reference: MustParseRef("data.capture.test"),
					Value:     BooleanTerm(true),
				},
				Body: NewBody(NewExpr(BooleanTerm(true))),
			},
		},
		captureFunc: func(input *Term) {
			receivedInput = input
		},
	}

	compiler := NewCompiler()
	compiler.WithExternalSource(packageRef, captureSource)

	testInput := ObjectTerm(Item(StringTerm("key"), StringTerm("value")))
	testRef := MustParseRef("data.test")

	index, err := captureSource.Init(t.Context())
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	rules, err := index.AllRules(t.Context(), testRef, testInput)
	if err != nil {
		t.Fatalf("AllRules failed: %v", err)
	}

	if len(rules) == 0 {
		t.Error("Expected rules from source")
	}

	if receivedInput == nil {
		t.Error("Expected input to be captured")
	} else if !receivedInput.Equal(testInput) {
		t.Errorf("Expected input %v, got %v", testInput, receivedInput)
	}
}

func TestMultipleExternalSources(t *testing.T) {
	rule1 := &Rule{
		Head: &Head{
			Reference: MustParseRef("data.pkg1.foo"),
			Value:     IntNumberTerm(1),
		},
		Body: NewBody(NewExpr(BooleanTerm(true))),
	}

	rule2 := &Rule{
		Head: &Head{
			Reference: MustParseRef("data.pkg2.bar"),
			Value:     IntNumberTerm(2),
		},
		Body: NewBody(NewExpr(BooleanTerm(true))),
	}

	source1 := newMockExternalSource([]Ref{MustParseRef("data.pkg1")}, []*Rule{rule1})
	source2 := newMockExternalSource([]Ref{MustParseRef("data.pkg2")}, []*Rule{rule2})

	compiler := NewCompiler()
	compiler.WithExternalSource(MustParseRef("data.pkg1"), source1)
	compiler.WithExternalSource(MustParseRef("data.pkg2"), source2)

	findExactSource := func(query Ref) ExternalRuleSource {
		var found ExternalRuleSource
		compiler.IterateExternalSources(func(pkgRef Ref, src ExternalRuleSource) bool {
			if query.Equal(pkgRef) {
				found = src
				return true
			}
			return false
		})
		return found
	}

	found1 := findExactSource(MustParseRef("data.pkg1"))
	if found1 != source1 {
		t.Error("Expected to find source1")
	}

	found2 := findExactSource(MustParseRef("data.pkg2"))
	if found2 != source2 {
		t.Error("Expected to find source2")
	}

	index1 := compiler.RuleIndex(MustParseRef("data.pkg1"))
	if index1 != nil {
		t.Error("Expected nil RuleIndex for external source pkg1")
	}

	index2 := compiler.RuleIndex(MustParseRef("data.pkg2"))
	if index2 != nil {
		t.Error("Expected nil RuleIndex for external source pkg2")
	}
}

func TestExternalSourceError(t *testing.T) {
	packageRef := MustParseRef("data.error.test")
	errorSource := &errorExternalSource{
		refs: []Ref{packageRef},
		err:  fmt.Errorf("simulated error"),
	}

	compiler := NewCompiler()
	compiler.WithExternalSource(packageRef, errorSource)

	var found ExternalRuleSource
	compiler.IterateExternalSources(func(pkgRef Ref, src ExternalRuleSource) bool {
		if pkgRef.Equal(packageRef) {
			found = src
			return true
		}
		return false
	})
	if found == nil {
		t.Error("Expected to find error source")
	}

	index := compiler.RuleIndex(packageRef)
	if index != nil {
		t.Error("Expected nil RuleIndex for external source")
	}

	idx, err := errorSource.Init(t.Context())
	if err == nil {
		t.Error("Expected error from Init")
	}
	if idx != nil {
		t.Error("Expected nil index from Init")
	}
}

type inputCapturingSource struct {
	refs        []Ref
	rules       []*Rule
	captureFunc func(*Term)
}

func (s *inputCapturingSource) Refs() []Ref {
	return s.refs
}

func (s *inputCapturingSource) Init(context.Context) (ExternalRuleIndex, error) {
	return &inputCapturingIndex{rules: s.rules, captureFunc: s.captureFunc}, nil
}

type inputCapturingIndex struct {
	rules       []*Rule
	captureFunc func(*Term)
}

func (s *inputCapturingIndex) Lookup(ctx context.Context, ref Ref, input *Term, resolver ValueResolver) ([]*Rule, error) {
	if s.captureFunc != nil {
		s.captureFunc(input)
	}
	return s.rules, nil
}

func (s *inputCapturingIndex) AllRules(ctx context.Context, ref Ref, input *Term) ([]*Rule, error) {
	if s.captureFunc != nil {
		s.captureFunc(input)
	}
	return s.rules, nil
}

type errorExternalSource struct {
	refs []Ref
	err  error
}

func (e *errorExternalSource) Refs() []Ref {
	return e.refs
}

func (e *errorExternalSource) Init(context.Context) (ExternalRuleIndex, error) {
	return nil, e.err
}
