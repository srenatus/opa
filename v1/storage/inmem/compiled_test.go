// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package inmem

import (
	"maps"
	"slices"
	"testing"

	"github.com/open-policy-agent/opa/v1/storage"
)

func TestCompiledRules(t *testing.T) {
	ctx := t.Context()
	s := New()
	store := s.(storage.CompiledRules)

	names := func(txn storage.Transaction) []string {
		kept, err := store.GetCompiledRules(ctx, txn)
		if err != nil {
			t.Fatal(err)
		}
		return slices.Sorted(maps.Keys(kept))
	}

	txn := storage.NewTransactionOrDie(ctx, s, storage.WriteParams)
	if err := store.UpsertCompiledRules(ctx, txn, "a", []byte("rules of a"), []string{"a.rego"}); err != nil {
		t.Fatal(err)
	}
	if kept, err := store.GetCompiledRules(ctx, txn); err != nil || string(kept["a"]) != "rules of a" {
		t.Fatalf("expected the transaction to see its write, got %q, %v", kept, err)
	}
	s.Abort(ctx, txn)

	txn = storage.NewTransactionOrDie(ctx, s, storage.WriteParams)
	if got := names(txn); len(got) != 0 {
		t.Fatalf("expected an aborted write to be gone, got %v", got)
	}
	for _, id := range []string{"a.rego", "a2.rego", "b.rego", "c.rego"} {
		_ = s.UpsertPolicy(ctx, txn, id, []byte("package "+id[:1]))
	}
	_ = store.UpsertCompiledRules(ctx, txn, "a", []byte("rules of a"), []string{"a.rego", "a2.rego"})
	_ = store.UpsertCompiledRules(ctx, txn, "b", []byte("rules of b"), []string{"b.rego"})
	_ = store.UpsertCompiledRules(ctx, txn, "c", []byte("rules of c"), []string{"c.rego"})
	if err := s.Commit(ctx, txn); err != nil {
		t.Fatal(err)
	}

	txn = storage.NewTransactionOrDie(ctx, s, storage.WriteParams)
	if err := s.UpsertPolicy(ctx, txn, "a2.rego", []byte("package a")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePolicy(ctx, txn, "c.rego"); err != nil {
		t.Fatal(err)
	}
	if got := names(txn); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("expected b only, got %v", got)
	}
	if err := s.Commit(ctx, txn); err != nil {
		t.Fatal(err)
	}

	txn = storage.NewTransactionOrDie(ctx, s)
	defer s.Abort(ctx, txn)
	if got := names(txn); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("expected b after commit, got %v", got)
	}
	if err := store.UpsertCompiledRules(ctx, txn, "d", nil, nil); err == nil {
		t.Fatal("expected a write in a read transaction to fail")
	}
}
