package topdown

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
)

type countingExternalSource struct {
	refs      []ast.Ref
	rules     []*ast.Rule
	callCount int32
}

func (m *countingExternalSource) Init(context.Context, ast.Ref) (ast.ExternalRuleIndex, error) {
	return &countingExternalIndex{rules: m.rules, callCount: &m.callCount}, nil
}

func (m *countingExternalSource) Refs() []ast.Ref {
	return m.refs
}

type countingExternalIndex struct {
	rules     []*ast.Rule
	callCount *int32
}

func (m *countingExternalIndex) Lookup(context.Context, ast.ValueResolver) ([]*ast.Rule, error) {
	atomic.AddInt32(m.callCount, 1)
	return m.rules, nil
}

func (m *countingExternalIndex) AllRules(context.Context, ast.ValueResolver) ([]*ast.Rule, error) {
	atomic.AddInt32(m.callCount, 1)
	return m.rules, nil
}

func (m *countingExternalSource) getCallCount() int {
	return int(atomic.LoadInt32(&m.callCount))
}

func (m *countingExternalSource) resetCount() {
	atomic.StoreInt32(&m.callCount, 0)
}

func compileExternalModule(t *testing.T, module *ast.Module) *ast.Module {
	t.Helper()
	compiler := ast.NewCompiler()
	compiler.Compile(map[string]*ast.Module{"external.rego": module})
	if compiler.Failed() {
		t.Fatalf("Module compilation failed: %v", compiler.Errors)
	}
	return compiler.Modules["external.rego"]
}

func setupCompiler(t *testing.T, packageRef ast.Ref, source ast.ExternalRuleSource, staticModule *ast.Module) *ast.Compiler {
	t.Helper()
	compiler := ast.NewCompiler()
	compiler.WithExternalSource(packageRef, source)
	modules := map[string]*ast.Module{}
	if staticModule != nil {
		modules["main.rego"] = staticModule
	}
	compiler.Compile(modules)
	if compiler.Failed() {
		t.Fatalf("Compiler failed: %v", compiler.Errors)
	}
	return compiler
}

func runQuery(t *testing.T, compiler *ast.Compiler, queryStr string, input *ast.Term) QueryResultSet {
	t.Helper()
	store := inmem.New()
	ctx := t.Context()
	txn, err := store.NewTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Abort(ctx, txn)

	query := ast.MustParseBody(queryStr)
	q := NewQuery(query).
		WithCompiler(compiler).
		WithStore(store).
		WithTransaction(txn).
		WithInput(input)

	qrs, err := q.Run(ctx)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	return qrs
}

func TestExternalSourceE2ESimple(t *testing.T) {
	t.Parallel()

	externalModule := ast.MustParseModule(`package test
foo if true`)

	packageRef := ast.MustParseRef("data.test")
	source := &countingExternalSource{refs: []ast.Ref{packageRef}, rules: externalModule.Rules}
	// cachedSource := sp.NewCachedSource(source)
	staticModule := ast.MustParseModule(`package main
result if data.test.foo`)

	compiler := setupCompiler(t, packageRef, source, staticModule)

	treeNode := compiler.RuleTree.Find(packageRef)
	if treeNode == nil {
		t.Fatal("Expected RuleTree to have node for external source path")
	}

	idx := compiler.RuleIndex(packageRef)
	if idx == nil {
		t.Fatal("Expected RuleIndex to return wrapped external index for external source path")
	}

	src := compiler.GetExternalSource(packageRef)
	if src == nil {
		t.Fatal("Expected GetExternalSource to return source")
	}

	queryRef := ast.MustParseRef("data.test.foo")
	if src2 := compiler.GetExternalSource(queryRef); src2 == nil {
		t.Fatalf("GetExternalSource(%v) returned nil", queryRef)
	}

	input := ast.MustParseTerm(`{}`)
	qrs := runQuery(t, compiler, "data.main.result", input)

	if len(qrs) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(qrs))
	}

	if callCount := source.getCallCount(); callCount != 1 {
		t.Errorf("Expected external source to be called once, got %d calls", callCount)
	}
}

func TestExternalSourceE2EWithDependencies(t *testing.T) {
	t.Parallel()

	externalModule := ast.MustParseModule(`package authz
allow if input.user == "alice"
deny if input.action == "delete"
allowed if {
	allow
	not deny
}`)

	packageRef := ast.MustParseRef("data.authz")
	compiledModule := compileExternalModule(t, externalModule)
	source := &countingExternalSource{refs: []ast.Ref{packageRef}, rules: compiledModule.Rules}

	staticModule := ast.MustParseModule(`package main
check if data.authz.allowed`)

	compiler := setupCompiler(t, packageRef, source, staticModule)

	testCase := func(t *testing.T, inputJSON string, callCountExp int, expectResult bool) {
		t.Helper()
		source.resetCount()

		input := ast.MustParseTerm(inputJSON)
		qrs := runQuery(t, compiler, "data.main.check", input)

		if (len(qrs) == 1) != expectResult {
			t.Fatalf("Expected one result, got %d", len(qrs))
		}

		if callCount := source.getCallCount(); callCount != callCountExp {
			t.Errorf("Expected external source to be called %d time(s), got %d calls", callCountExp, callCount)
		}
	}

	t.Run("alice_read_allowed", func(t *testing.T) {
		testCase(t, `{"user": "alice", "action": "read"}`, 3, true)
	})
	t.Run("alice_delete_denied", func(t *testing.T) {
		testCase(t, `{"user": "alice", "action": "delete"}`, 2, false)
	})
	t.Run("bob_read_denied", func(t *testing.T) {
		testCase(t, `{"user": "bob", "action": "read"}`, 2, false)
	})
}

func TestExternalSourceE2EMultipleRefs(t *testing.T) {
	t.Parallel()

	externalModule := ast.MustParseModule(`package test
check1 if input.a == 1
check2 if input.b == 2
check3 if input.c == 3
result if {
	check1
	check2
	check3
}`)

	packageRef := ast.MustParseRef("data.test")
	source := &countingExternalSource{refs: []ast.Ref{packageRef}, rules: externalModule.Rules}
	// cachedSource := sp.NewCachedSource(source)
	staticModule := ast.MustParseModule(`package main
verify if data.test.result`)

	compiler := setupCompiler(t, packageRef, source, staticModule)

	input := ast.MustParseTerm(`{"a": 1, "b": 2, "c": 3}`)
	qrs := runQuery(t, compiler, "data.main.verify", input)

	if len(qrs) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(qrs))
	}

	if callCount := source.getCallCount(); callCount != 1 {
		t.Errorf("Expected external source to be called once despite multiple rule references, got %d calls", callCount)
	}
}

func TestExternalSourceE2EWithInputOverride(t *testing.T) {
	t.Parallel()

	externalModule := ast.MustParseModule(`package authz
allowed if input.user == "alice"`)

	packageRef := ast.MustParseRef("data.authz")
	compiledModule := compileExternalModule(t, externalModule)
	source := &countingExternalSource{refs: []ast.Ref{packageRef}, rules: compiledModule.Rules}

	staticModule := ast.MustParseModule(`package main
check if {
	data.authz.allowed
	data.authz.allowed with input as {"user": "bob"}
}`)

	compiler := setupCompiler(t, packageRef, source, staticModule)

	input := ast.MustParseTerm(`{"user": "alice"}`)
	qrs := runQuery(t, compiler, "data.main.check", input)

	if len(qrs) != 0 {
		t.Errorf("Expected 0 results (second check with bob should fail), got %d", len(qrs))
	}

	if callCount := source.getCallCount(); callCount != 2 {
		t.Errorf("Expected external source to be called twice (once per input), got %d calls", callCount)
	}
}

func TestExternalSourceE2EWithMultipleRulesFromSamePackage(t *testing.T) {
	t.Parallel()

	externalModule := ast.MustParseModule(`package authz
allow if input.user == "alice"
deny if input.action == "delete"
allowed if {
	allow
	not deny
}`)

	packageRef := ast.MustParseRef("data.authz")
	compiledModule := compileExternalModule(t, externalModule)
	source := &countingExternalSource{refs: []ast.Ref{packageRef}, rules: compiledModule.Rules}
	// cachedSource := sp.NewCachedSource(source)

	staticModule := ast.MustParseModule(`package main
check if data.authz.allowed`)

	compiler := setupCompiler(t, packageRef, source, staticModule)

	input := ast.MustParseTerm(`{"user": "alice", "action": "read"}`)
	qrs := runQuery(t, compiler, "data.main.check", input)

	if len(qrs) != 1 {
		t.Errorf("Expected 1 result, got %d", len(qrs))
	}

	if callCount := source.getCallCount(); callCount != 3 {
		t.Errorf("Expected external source to be called 3 times (once per rule: allowed, allow, deny), got %d calls", callCount)
	}
}

func TestExternalSourceE2EWithInputOverrideViaStaticRule(t *testing.T) {
	t.Parallel()

	externalModule := ast.MustParseModule(`package authz
allowed if input.user == "alice"`)

	packageRef := ast.MustParseRef("data.authz")
	compiledModule := compileExternalModule(t, externalModule)
	source := &countingExternalSource{refs: []ast.Ref{packageRef}, rules: compiledModule.Rules}

	staticModule := ast.MustParseModule(`package main
allow if {
	data.authz.allowed
	data.authz.allowed with input as {"user": "bob"}
}`)

	compiler := setupCompiler(t, packageRef, source, staticModule)

	input := ast.MustParseTerm(`{"user": "alice"}`)
	qrs := runQuery(t, compiler, "data.main.allow", input)

	if len(qrs) != 0 {
		t.Errorf("Expected 0 results (second check with bob should fail), got %d", len(qrs))
	}

	if callCount := source.getCallCount(); callCount != 2 {
		t.Errorf("Expected external source to be called twice (once per input), got %d calls", callCount)
	}
}
