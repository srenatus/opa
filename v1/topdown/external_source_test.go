package topdown

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
)

type countingExternalSource struct {
	rules     []*ast.Rule
	callCount int32
}

func (m *countingExternalSource) GetRules(ctx context.Context, input *ast.Term) ([]*ast.Rule, error) {
	atomic.AddInt32(&m.callCount, 1)
	return m.rules, nil
}

func (m *countingExternalSource) getCallCount() int {
	return int(atomic.LoadInt32(&m.callCount))
}

func TestExternalSourceE2ESimple(t *testing.T) {
	t.Parallel()

	// Simple test: single rule that always succeeds
	module := ast.MustParseModule(`package test

foo if true`)

	// No need to set Head.Reference for simple rules
	// The rule name is sufficient, and rule.Ref() will combine
	// Package.Path with the rule name automatically

	source := &countingExternalSource{
		rules:     module.Rules,
		callCount: 0,
	}

	compiler := ast.NewCompiler()
	packageRef := ast.MustParseRef("data.test")
	compiler.WithExternalSource(packageRef, source)
	compiler.Compile(map[string]*ast.Module{})

	// Check that the tree node was added
	treeNode := compiler.RuleTree.Find(packageRef)
	if treeNode == nil {
		t.Fatal("Expected RuleTree to have node for external source path")
	}
	t.Logf("TreeNode found: Children=%d, Values=%d", len(treeNode.Children), len(treeNode.Values))

	// Verify RuleIndex returns nil for external source path
	idx := compiler.RuleIndex(packageRef)
	if idx != nil {
		t.Fatalf("Expected RuleIndex to return nil for external source path, got %v", idx)
	}

	// Verify GetExternalSource works
	src, ref := compiler.GetExternalSource(packageRef)
	if src == nil {
		t.Fatal("Expected GetExternalSource to return source")
	}
	if !ref.Equal(packageRef) {
		t.Fatalf("Expected ref %v, got %v", packageRef, ref)
	}

	// Check what getRules would find
	queryRef := ast.MustParseRef("data.test.foo")
	src2, ref2 := compiler.GetExternalSource(queryRef)
	if src2 == nil {
		t.Fatalf("GetExternalSource(%v) returned nil, but expected to match", queryRef)
	}
	t.Logf("GetExternalSource(%v) matched ref %v", queryRef, ref2)

	input := ast.MustParseTerm(`{}`)
	store := inmem.New()
	ctx := context.Background()
	txn, err := store.NewTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Abort(ctx, txn)

	query := ast.MustParseBody("data.test.foo")
	q := NewQuery(query).
		WithCompiler(compiler).
		WithStore(store).
		WithTransaction(txn).
		WithInput(input)

	qrs, err := q.Run(ctx)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	t.Logf("Query results: %d", len(qrs))
	t.Logf("External source call count: %d", source.getCallCount())

	if len(qrs) != 1 {
		t.Fatalf("Expected 1 result, got %d. External source was called %d times", len(qrs), source.getCallCount())
	}

	callCount := source.getCallCount()
	if callCount != 1 {
		t.Errorf("Expected external source to be called exactly ONCE, got %d calls", callCount)
	}
}

func TestExternalSourceE2EWithDependencies(t *testing.T) {
	t.Parallel()

	// Parse and compile the module so refs are properly resolved
	module := ast.MustParseModule(`package authz

allow if input.user == "alice"
deny if input.action == "delete"
allowed if {
	allow
	not deny
}`)

	// Compile the module to resolve cross-references between rules
	tempCompiler := ast.NewCompiler()
	tempCompiler.Compile(map[string]*ast.Module{"test.rego": module})
	if tempCompiler.Failed() {
		t.Fatalf("Module compilation failed: %v", tempCompiler.Errors)
	}

	// Use the compiled module which has fully qualified refs
	compiledModule := tempCompiler.Modules["test.rego"]

	source := &countingExternalSource{
		rules:     compiledModule.Rules,
		callCount: 0,
	}

	compiler := ast.NewCompiler()
	packageRef := ast.MustParseRef("data.authz")
	compiler.WithExternalSource(packageRef, source)
	compiler.Compile(map[string]*ast.Module{})

	// Test case 1: user=alice, action=read -> should allow
	testCase1 := func(t *testing.T) {
		t.Helper()
		source.callCount = 0 // Reset counter

		input := ast.MustParseTerm(`{"user": "alice", "action": "read"}`)
		store := inmem.New()
		ctx := context.Background()
		txn, err := store.NewTransaction(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Abort(ctx, txn)

		query := ast.MustParseBody("data.authz.allowed")
		q := NewQuery(query).
			WithCompiler(compiler).
			WithStore(store).
			WithTransaction(txn).
			WithInput(input)

		qrs, err := q.Run(ctx)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		if len(qrs) != 1 {
			t.Fatalf("Expected 1 result, got %d", len(qrs))
		}

		callCount := source.getCallCount()
		if callCount != 1 {
			t.Errorf("Expected external source to be called exactly ONCE, got %d calls", callCount)
		}
	}

	// Test case 2: user=alice, action=delete -> should deny
	testCase2 := func(t *testing.T) {
		t.Helper()
		source.callCount = 0 // Reset counter

		input := ast.MustParseTerm(`{"user": "alice", "action": "delete"}`)
		store := inmem.New()
		ctx := context.Background()
		txn, err := store.NewTransaction(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Abort(ctx, txn)

		query := ast.MustParseBody("data.authz.allowed")
		q := NewQuery(query).
			WithCompiler(compiler).
			WithStore(store).
			WithTransaction(txn).
			WithInput(input)

		qrs, err := q.Run(ctx)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		if len(qrs) != 0 {
			t.Fatalf("Expected 0 results (denied), got %d", len(qrs))
		}

		callCount := source.getCallCount()
		if callCount != 1 {
			t.Errorf("Expected external source to be called exactly ONCE, got %d calls", callCount)
		}
	}

	// Test case 3: user=bob, action=read -> should deny (user not allowed)
	testCase3 := func(t *testing.T) {
		t.Helper()
		source.callCount = 0 // Reset counter

		input := ast.MustParseTerm(`{"user": "bob", "action": "read"}`)
		store := inmem.New()
		ctx := context.Background()
		txn, err := store.NewTransaction(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Abort(ctx, txn)

		query := ast.MustParseBody("data.authz.allowed")
		q := NewQuery(query).
			WithCompiler(compiler).
			WithStore(store).
			WithTransaction(txn).
			WithInput(input)

		qrs, err := q.Run(ctx)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}

		if len(qrs) != 0 {
			t.Fatalf("Expected 0 results (user not allowed), got %d", len(qrs))
		}

		callCount := source.getCallCount()
		if callCount != 1 {
			t.Errorf("Expected external source to be called exactly ONCE, got %d calls", callCount)
		}
	}

	t.Run("alice_read_allowed", testCase1)
	t.Run("alice_delete_denied", testCase2)
	t.Run("bob_read_denied", testCase3)
}

func TestExternalSourceE2EMultipleRefs(t *testing.T) {
	t.Parallel()

	// Parse rules from Rego
	module := ast.MustParseModule(`package test

check1 if input.a == 1
check2 if input.b == 2
check3 if input.c == 3

result if {
	check1
	check2
	check3
}`)

	source := &countingExternalSource{
		rules:     module.Rules,
		callCount: 0,
	}

	compiler := ast.NewCompiler()
	packageRef := ast.MustParseRef("data.test")
	compiler.WithExternalSource(packageRef, source)
	compiler.Compile(map[string]*ast.Module{})

	input := ast.MustParseTerm(`{"a": 1, "b": 2, "c": 3}`)
	store := inmem.New()
	ctx := context.Background()
	txn, err := store.NewTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Abort(ctx, txn)

	query := ast.MustParseBody("data.test.result")
	q := NewQuery(query).
		WithCompiler(compiler).
		WithStore(store).
		WithTransaction(txn).
		WithInput(input)

	qrs, err := q.Run(ctx)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(qrs) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(qrs))
	}

	callCount := source.getCallCount()
	if callCount != 1 {
		t.Errorf("Expected external source to be called exactly ONCE despite multiple rule references, got %d calls", callCount)
	}
}
