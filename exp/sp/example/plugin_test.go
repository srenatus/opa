// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package exampleplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/plugins"
	"github.com/open-policy-agent/opa/v1/sdk"
	sdktest "github.com/open-policy-agent/opa/v1/sdk/test"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
	"github.com/open-policy-agent/opa/v1/topdown"
)

func TestPluginLifecycle(t *testing.T) {
	ctx := context.Background()

	store := inmem.New()
	manager, err := plugins.New([]byte(`{}`), "test", store)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	pluginConfig := Config{
		PackageRef: "data.external.authz",
		Rules: []string{
			`package external.authz

allow := true if { input.role == "admin" }`,
		},
	}

	configBytes, err := json.Marshal(pluginConfig)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	factory := Factory{}
	parsedConfig, err := factory.Validate(manager, configBytes)
	if err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}

	plugin := factory.New(manager, parsedConfig)
	manager.Register(PluginName, plugin)

	if err := manager.Init(ctx); err != nil {
		t.Fatalf("Failed to initialize manager: %v", err)
	}

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	status := manager.PluginStatus()[PluginName]
	if status == nil {
		t.Fatal("Plugin status not found")
	}
	if status.State != plugins.StateOK {
		t.Errorf("Expected plugin state OK, got %v: %v", status.State, status.Message)
	}

	compiler := manager.GetCompiler()
	if compiler == nil {
		t.Fatal("Compiler is nil")
	}

	pkgRef := ast.MustParseRef("data.external.authz")
	externalSource := compiler.GetExternalSource(pkgRef)
	if externalSource == nil {
		t.Fatal("External source not registered with compiler")
	}

	index, err := externalSource.Init(ctx, pkgRef)
	if err != nil {
		t.Fatalf("Failed to initialize external source: %v", err)
	}

	rules, err := index.AllRules(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to get rules: %v", err)
	}

	if len(rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(rules))
	}

	manager.Stop(ctx)
}

func TestConfigValidation(t *testing.T) {
	store := inmem.New()
	manager, err := plugins.New([]byte(`{}`), "test", store)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	factory := Factory{}

	tests := []struct {
		name    string
		config  string
		wantErr bool
	}{
		{
			name: "valid config",
			config: `{
				"package_ref": "data.test",
				"rules": ["package test\nx := 1 if { true }"]
			}`,
			wantErr: false,
		},
		{
			name: "missing package_ref",
			config: `{
				"rules": ["package test\nx := 1"]
			}`,
			wantErr: true,
		},
		{
			name: "invalid package_ref",
			config: `{
				"package_ref": "invalid!ref",
				"rules": []
			}`,
			wantErr: true,
		},
		{
			name:    "empty config",
			config:  `{}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := factory.Validate(manager, []byte(tt.config))
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMultipleRules(t *testing.T) {
	ctx := context.Background()

	store := inmem.New()
	manager, err := plugins.New([]byte(`{}`), "test", store)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	pluginConfig := Config{
		PackageRef: "data.external.multi",
		Rules: []string{
			`package external.multi

rule1 := 1 if { true }`,
			`package external.multi

rule2 := 2 if { true }`,
			`package external.multi

rule3 := 3 if { true }`,
		},
	}

	configBytes, _ := json.Marshal(pluginConfig)
	factory := Factory{}
	parsedConfig, _ := factory.Validate(manager, configBytes)
	plugin := factory.New(manager, parsedConfig)

	manager.Register(PluginName, plugin)
	manager.Init(ctx)
	manager.Start(ctx)
	defer manager.Stop(ctx)

	compiler := manager.GetCompiler()
	source := compiler.GetExternalSource(ast.MustParseRef("data.external.multi"))
	if source == nil {
		t.Fatal("External source not found")
	}

	pkgRef := ast.MustParseRef("data.external.multi")
	index, _ := source.Init(ctx, pkgRef)
	rules, _ := index.AllRules(ctx, nil)

	if len(rules) != 3 {
		t.Errorf("Expected 3 rules, got %d", len(rules))
	}
}

func TestPluginWithSDK(t *testing.T) {
	ctx := t.Context()
	server := sdktest.MustNewServer(
		sdktest.MockBundle("/bundles/bundle.tar.gz", map[string]string{
			"main.rego": `package authz
default allow := false
allow if data.external.authz.allow
`,
		}),
	)

	defer server.Stop()

	config := fmt.Sprintf(`{
		"services": {
			"test": {
				"url": %q
			}
		},
		"bundles": {
			"test": {
				"resource": "/bundles/bundle.tar.gz"
			}
		},
		"plugins": {
			"example_source_provider": {
				"package_ref": "data.external.authz",
				"rules": [
					"package external.authz\n\nimport rego.v1\n\nallow if { input.role == \"admin\" }"
				]
			}
		}
	}`, server.URL())

	opa, err := sdk.New(ctx, sdk.Options{
		Config: strings.NewReader(config),
		Plugins: map[string]plugins.Factory{
			PluginName: Factory{},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create SDK instance: %v", err)
	}
	defer opa.Stop(ctx)

	t.Run("admin role allowed", func(t *testing.T) {
		tracer := topdown.NewBufferTracer()
		result, err := opa.Decision(ctx, sdk.DecisionOptions{
			Path: "authz/allow",
			Input: map[string]any{
				"role": "admin",
			},
			Tracer: tracer,
		})
		if err != nil {
			t.Fatalf("Decision failed: %v", err)
		}

		if result.Result != true {
			t.Errorf("Expected allow=true for admin role, got %v", result.Result)
		}

		topdown.PrettyTrace(os.Stderr, *tracer)
	})

	t.Run("non-admin role not allowed", func(t *testing.T) {
		tracer := topdown.NewBufferTracer()
		result, err := opa.Decision(ctx, sdk.DecisionOptions{
			Path: "authz/allow",
			Input: map[string]any{
				"role": "user",
			},
			Tracer: tracer,
		})
		if err != nil {
			t.Fatalf("Decision failed: %v", err)
		}

		if result.Result != false {
			t.Errorf("Expected allow=false for user role, got %v", result.Result)
		}

		topdown.PrettyTrace(os.Stderr, *tracer)
	})

	t.Run("partial eval into external rule", func(t *testing.T) {
		tracer := topdown.NewBufferTracer()
		result, err := opa.Partial(ctx, sdk.PartialOptions{
			Query:    "data.authz.allow",
			Unknowns: []string{"input"},
			Tracer:   tracer,
		})
		if err != nil {
			t.Fatal(err)
		}
		topdown.PrettyTrace(os.Stderr, *tracer)

		if result.AST == nil {
			t.Fatal("expected AST, got nil")
		}
		t.Logf("Result: %v", result.AST.Queries[0][0].Terms)
	})
}
