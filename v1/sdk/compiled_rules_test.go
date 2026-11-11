// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package sdk

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/logging"
)

// TestDecisionCompiledRules reads a bundle with compiled rule sets from a file,
// as the bundle plugin does for the SDK.
func TestDecisionCompiledRules(t *testing.T) {
	for _, generated := range []bool{false, true} {
		t.Run(fmt.Sprintf("generated=%v", generated), func(t *testing.T) {
			var src strings.Builder
			src.WriteString("package authz\n\n")
			for i := range 1000 {
				fmt.Fprintf(&src, "allow if input.user == \"u%d\"\n", i)
			}
			b := bundle.Bundle{
				Data:     map[string]any{},
				Manifest: bundle.Manifest{Revision: "1", Generated: generated},
				Modules:  []bundle.ModuleFile{{Path: "/authz.rego", URL: "/authz.rego", Raw: []byte(src.String())}},
			}
			if err := b.CompileRules(nil, nil); err != nil || b.CompiledRules == nil {
				t.Fatalf("expected compiled rules, got %d bytes (%v)", len(b.CompiledRules), err)
			}
			var buf bytes.Buffer
			if err := bundle.NewWriter(&buf).Write(b); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "bundle.tar.gz"), buf.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}

			ready := make(chan struct{})
			opa, err := New(t.Context(), Options{
				Ready:  ready,
				Logger: logging.NewNoOpLogger(),
				Config: strings.NewReader(fmt.Sprintf("bundles:\n  test:\n    resource: \"file://%s/bundle.tar.gz\"\n", dir)),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer opa.Stop(t.Context())
			<-ready

			if n := len(opa.state.manager.GetCompiler().LazyRuleSets()); n != 1 {
				t.Fatalf("expected the compiled rule set to be used, got %d lazy rule sets", n)
			}
			for user, exp := range map[string]bool{"u500": true, "u999": true, "nobody": false} {
				result, err := opa.Decision(t.Context(), DecisionOptions{Path: "/authz/allow", Input: map[string]any{"user": user}})
				if got := err == nil && result.Result == true; got != exp {
					t.Errorf("user %s: expected allow %v, got %v (%v)", user, exp, result, err)
				}
			}
		})
	}
}
