package rego

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
)

type mockExternalSource struct {
	refs      []ast.Ref
	rules     []*ast.Rule
	callCount int32
}

func newMockExternalSource(refs []ast.Ref, rules []*ast.Rule) *mockExternalSource {
	return &mockExternalSource{
		refs:      refs,
		rules:     rules,
		callCount: 0,
	}
}

func (m *mockExternalSource) Refs() []ast.Ref {
	return m.refs
}

func (m *mockExternalSource) Init(context.Context) (ast.ExternalRuleIndex, error) {
	return &mockExternalIndex{rules: m.rules, callCount: &m.callCount}, nil
}

type mockExternalIndex struct {
	rules     []*ast.Rule
	callCount *int32
}

func (m *mockExternalIndex) Lookup(ctx context.Context, input *ast.Term, resolver ast.ValueResolver) ([]*ast.Rule, error) {
	atomic.AddInt32(m.callCount, 1)
	return m.rules, nil
}

func (m *mockExternalIndex) AllRules(ctx context.Context, input *ast.Term) ([]*ast.Rule, error) {
	atomic.AddInt32(m.callCount, 1)
	return m.rules, nil
}

func (m *mockExternalSource) getCallCount() int {
	return int(atomic.LoadInt32(&m.callCount))
}

func TestExternalSourceViaRegoAPI(t *testing.T) {
	ctx := context.Background()

	rule := &ast.Rule{
		Head: &ast.Head{
			Reference: ast.MustParseRef("data.external.test.allow"),
			Value:     ast.BooleanTerm(true),
		},
		Body: ast.NewBody(
			ast.Equality.Expr(ast.VarTerm("x"), ast.IntNumberTerm(42)),
		),
	}

	packageRef := ast.MustParseRef("data.external.test")
	source := newMockExternalSource([]ast.Ref{packageRef}, []*ast.Rule{rule})

	r := New(
		Query("data.external.test.allow"),
		ExternalSource(source),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		t.Fatalf("PrepareForEval failed: %v", err)
	}

	if pq.r.compiler == nil {
		t.Fatal("Expected compiler to be set")
	}

	externalSource := pq.r.compiler.GetExternalSource(packageRef)
	if externalSource == nil {
		t.Fatal("Expected external source to be registered with compiler")
	}

	if externalSource != source {
		t.Error("Expected external source to match the one we registered")
	}
}

func TestMultipleExternalSourcesViaRegoAPI(t *testing.T) {
	ctx := context.Background()

	rule1 := &ast.Rule{
		Head: &ast.Head{
			Reference: ast.MustParseRef("data.pkg1.foo"),
			Value:     ast.IntNumberTerm(1),
		},
		Body: ast.NewBody(ast.NewExpr(ast.BooleanTerm(true))),
	}

	rule2 := &ast.Rule{
		Head: &ast.Head{
			Reference: ast.MustParseRef("data.pkg2.bar"),
			Value:     ast.IntNumberTerm(2),
		},
		Body: ast.NewBody(ast.NewExpr(ast.BooleanTerm(true))),
	}

	source1 := newMockExternalSource([]ast.Ref{ast.MustParseRef("data.pkg1")}, []*ast.Rule{rule1})
	source2 := newMockExternalSource([]ast.Ref{ast.MustParseRef("data.pkg2")}, []*ast.Rule{rule2})

	r := New(
		Query("data.pkg1.foo"),
		ExternalSource(source1),
		ExternalSource(source2),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		t.Fatalf("PrepareForEval failed: %v", err)
	}

	src1 := pq.r.compiler.GetExternalSource(ast.MustParseRef("data.pkg1"))
	if src1 != source1 {
		t.Error("Expected to find source1 at data.pkg1")
	}

	src2 := pq.r.compiler.GetExternalSource(ast.MustParseRef("data.pkg2"))
	if src2 != source2 {
		t.Error("Expected to find source2 at data.pkg2")
	}
}

func TestExternalSourceWithStaticModule(t *testing.T) {
	ctx := context.Background()

	rule := &ast.Rule{
		Head: &ast.Head{
			Reference: ast.MustParseRef("data.external.dynamic_rule"),
			Value:     ast.BooleanTerm(true),
		},
		Body: ast.NewBody(
			ast.Equality.Expr(ast.VarTerm("input.value"), ast.IntNumberTerm(100)),
		),
	}

	source := newMockExternalSource([]ast.Ref{ast.MustParseRef("data.external")}, []*ast.Rule{rule})

	staticModule := `package static
	allow if {
		data.external.dynamic_rule
	}`

	r := New(
		Query("data.static.allow"),
		Module("static.rego", staticModule),
		ExternalSource(source),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		t.Fatalf("PrepareForEval failed: %v", err)
	}

	externalSource := pq.r.compiler.GetExternalSource(ast.MustParseRef("data.external"))
	if externalSource == nil {
		t.Fatal("Expected external source to be registered")
	}

	if externalSource != source {
		t.Error("External source should match the one provided")
	}

	if pq.r.compiler.Modules["static.rego"] == nil {
		t.Error("Expected static module to be compiled")
	}
}

func TestExternalSourceMultipleRefs(t *testing.T) {
	ctx := context.Background()

	rule1 := &ast.Rule{
		Head: &ast.Head{
			Reference: ast.MustParseRef("data.pkg1.foo"),
			Value:     ast.IntNumberTerm(1),
		},
		Body: ast.NewBody(ast.NewExpr(ast.BooleanTerm(true))),
	}

	rule2 := &ast.Rule{
		Head: &ast.Head{
			Reference: ast.MustParseRef("data.pkg2.bar"),
			Value:     ast.IntNumberTerm(2),
		},
		Body: ast.NewBody(ast.NewExpr(ast.BooleanTerm(true))),
	}

	// Single source provides rules for multiple packages
	source := newMockExternalSource(
		[]ast.Ref{ast.MustParseRef("data.pkg1"), ast.MustParseRef("data.pkg2")},
		[]*ast.Rule{rule1, rule2},
	)

	r := New(
		Query("data.pkg1.foo"),
		ExternalSource(source),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		t.Fatalf("PrepareForEval failed: %v", err)
	}

	// Verify both packages are registered with the same source
	src1 := pq.r.compiler.GetExternalSource(ast.MustParseRef("data.pkg1"))
	if src1 != source {
		t.Error("Expected to find source at data.pkg1")
	}

	src2 := pq.r.compiler.GetExternalSource(ast.MustParseRef("data.pkg2"))
	if src2 != source {
		t.Error("Expected to find source at data.pkg2")
	}

	if src1 != src2 {
		t.Error("Expected both packages to use the same source instance")
	}
}

func TestExternalSourcePrefixMatching(t *testing.T) {
	ctx := context.Background()

	rule := &ast.Rule{
		Head: &ast.Head{
			Reference: ast.MustParseRef("data.external.pkg.foo"),
			Value:     ast.BooleanTerm(true),
		},
		Body: ast.NewBody(ast.Equality.Expr(ast.VarTerm("x"), ast.IntNumberTerm(1))),
	}

	packageRef := ast.MustParseRef("data.external.pkg")
	source := newMockExternalSource([]ast.Ref{packageRef}, []*ast.Rule{rule})

	r := New(
		Query("data.external.pkg.foo"),
		ExternalSource(source),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		t.Fatalf("PrepareForEval failed: %v", err)
	}

	found := pq.r.compiler.GetExternalSource(packageRef)
	if found != source {
		t.Error("Expected to find source for exact package match")
	}

	subPath := ast.MustParseRef("data.external.pkg.foo")
	found = pq.r.compiler.GetExternalSource(subPath)
	if found != source {
		t.Error("Expected to find source for sub-path (prefix match)")
	}

	otherPath := ast.MustParseRef("data.other.pkg")
	found = pq.r.compiler.GetExternalSource(otherPath)
	if found != nil {
		t.Error("Expected nil for non-matching path")
	}
}
