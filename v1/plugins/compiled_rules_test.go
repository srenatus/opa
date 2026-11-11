// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package plugins

import (
	"fmt"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/storage"
	inmem "github.com/open-policy-agent/opa/v1/storage/inmem/test"
)

// Policy writes without a compiler, like the server's, compile with the store's
// compiled rule sets.
func TestManagerCompilesWithStoredCompiledRules(t *testing.T) {
	ctx := t.Context()
	var src strings.Builder
	src.WriteString("package authz\n\n")
	for i := range 1000 { // the fewest rules a rule set is compiled with
		fmt.Fprintf(&src, "allow if input.user == \"user-%d\"\n", i)
	}
	b := bundle.Bundle{
		Manifest: bundle.Manifest{Generated: true},
		Modules:  []bundle.ModuleFile{{Path: "authz.rego", URL: "authz.rego", Raw: []byte(src.String())}},
	}
	if err := b.CompileRules(nil, nil); err != nil {
		t.Fatal(err)
	}

	m, err := New([]byte{}, "test", inmem.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	txn := storage.NewTransactionOrDie(ctx, m.Store, storage.WriteParams)
	if err := m.Store.UpsertPolicy(ctx, txn, "authz.rego", b.Modules[0].Raw); err != nil {
		t.Fatal(err)
	}
	if err := m.Store.(storage.CompiledRules).UpsertCompiledRules(ctx, txn, "authz", b.CompiledRules, []string{"authz.rego"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Store.Commit(ctx, txn); err != nil {
		t.Fatal(err)
	}

	c := m.GetCompiler()
	if c == nil || len(c.LazyRuleSets()) != 1 || len(c.Modules["authz.rego"].Rules) != 0 {
		t.Fatalf("expected a compiler with the stored rule set, got %v", c)
	}
}
