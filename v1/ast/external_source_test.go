package ast

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

type mockExternalSource struct {
	rules     []*Rule
	callCount int32
	mu        sync.Mutex
}

func newMockExternalSource(rules []*Rule) *mockExternalSource {
	return &mockExternalSource{
		rules:     rules,
		callCount: 0,
	}
}

func (m *mockExternalSource) GetRules(input *Term) ([]*Rule, error) {
	atomic.AddInt32(&m.callCount, 1)
	return m.rules, nil
}

func (m *mockExternalSource) getCallCount() int {
	return int(atomic.LoadInt32(&m.callCount))
}

func (m *mockExternalSource) resetCallCount() {
	atomic.StoreInt32(&m.callCount, 0)
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

	source := newMockExternalSource([]*Rule{rule})
	compiler := NewCompiler()
	packageRef := MustParseRef("data.external.test")
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

	source := newMockExternalSource([]*Rule{rule})

	compiler := NewCompiler()
	packageRef := MustParseRef("data.external.pkg")
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

	source1 := newMockExternalSource([]*Rule{rule1})
	source2 := newMockExternalSource([]*Rule{rule2})

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

func TestBuildRuleIndexFromRules(t *testing.T) {
	rule1 := &Rule{
		Head: &Head{
			Reference: MustParseRef("data.test.foo"),
			Value:     IntNumberTerm(1),
		},
		Body: NewBody(NewExpr(BooleanTerm(true))),
	}

	rule2 := &Rule{
		Head: &Head{
			Reference: MustParseRef("data.test.bar"),
			Value:     IntNumberTerm(2),
		},
		Body: NewBody(NewExpr(BooleanTerm(true))),
	}

	compiler := NewCompiler()
	compiler.Compile(map[string]*Module{})
	rules := []*Rule{rule1, rule2}

	index := compiler.BuildRuleIndexFromRules(rules)
	if index == nil {
		t.Fatal("Expected non-nil index from BuildRuleIndexFromRules")
	}

	emptyIndex := compiler.BuildRuleIndexFromRules([]*Rule{})
	if emptyIndex != nil {
		t.Error("Expected nil index for empty rule set")
	}
}

func TestExternalSourceInputParameter(t *testing.T) {
	var receivedInput *Term

	captureSource := &inputCapturingSource{
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
	compiler.WithExternalSource(MustParseRef("data.capture"), captureSource)

	testInput := ObjectTerm(Item(StringTerm("key"), StringTerm("value")))

	rules, err := captureSource.GetRules(testInput)
	if err != nil {
		t.Fatalf("GetRules failed: %v", err)
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

	source1 := newMockExternalSource([]*Rule{rule1})
	source2 := newMockExternalSource([]*Rule{rule2})

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
	errorSource := &errorExternalSource{err: fmt.Errorf("simulated error")}

	compiler := NewCompiler()
	packageRef := MustParseRef("data.error.test")
	compiler.WithExternalSource(packageRef, errorSource)

	var found ExternalRuleSource
	compiler.IterateExternalSources(func(pkgRef Ref, src ExternalRuleSource) bool {
		if pkgRef.Equal(packageRef) {
			found = src
			return true
		}
		return false
	})
	if found != errorSource {
		t.Error("Expected to find error source")
	}

	index := compiler.RuleIndex(packageRef)
	if index != nil {
		t.Error("Expected nil RuleIndex for external source")
	}

	_, err := errorSource.GetRules(nil)
	if err == nil {
		t.Error("Expected error from GetRules")
	}
}

type inputCapturingSource struct {
	rules       []*Rule
	captureFunc func(*Term)
}

func (s *inputCapturingSource) GetRules(input *Term) ([]*Rule, error) {
	if s.captureFunc != nil {
		s.captureFunc(input)
	}
	return s.rules, nil
}

type errorExternalSource struct {
	err error
}

func (e *errorExternalSource) GetRules(input *Term) ([]*Rule, error) {
	return nil, e.err
}
