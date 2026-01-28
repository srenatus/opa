// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package main demonstrates compiler state serialization for faster CLI startup.
// This example shows how to compile policies once, serialize the state, and
// reload it quickly on subsequent runs.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/version"
)

// Example policies for demonstration
var examplePolicies = map[string]string{
	"authz.rego": `package authz

# Authorization rules for a multi-tenant application

import rego.v1

# Admin users have full access
allow if {
	input.user.role == "admin"
}

# Regular users can only access their own tenant's data
allow if {
	input.user.role == "user"
	input.user.tenant_id == input.resource.tenant_id
}

# Users can read public resources
allow if {
	input.action == "read"
	input.resource.public == true
}
`,
	"rbac.rego": `package rbac

import rego.v1

# Role-based access control rules

roles := {
	"admin": ["read", "write", "delete"],
	"editor": ["read", "write"],
	"viewer": ["read"],
}

permitted_actions contains action if {
	some action in roles[input.user.role]
}

allow if {
	input.action in permitted_actions
}
`,
	"audit.rego": `package audit

import rego.v1

# Audit logging requirements

requires_audit if {
	input.action in {"write", "delete"}
}

requires_audit if {
	input.user.role == "admin"
}

audit_metadata := {
	"timestamp": time.now_ns(),
	"user": input.user.id,
	"action": input.action,
	"resource": input.resource.id,
}
`,
}

func main() {
	stateFile := "compiler-state.json"

	fmt.Println("=== OPA Compiler State Serialization Example ===\n")

	// Check if we have a saved state file
	if data, err := os.ReadFile(stateFile); err == nil {
		fmt.Println("Found saved compiler state, attempting to load...")
		if loadedCompiler, err := loadFromSerializedState(data); err == nil {
			fmt.Println("✓ Successfully loaded from serialized state (fast path!)")
			demonstrateCompiler(loadedCompiler)
			return
		} else {
			fmt.Printf("✗ Failed to load from state: %v\n", err)
			fmt.Println("  Falling back to full compilation...\n")
		}
	}

	// No saved state or loading failed - compile from source
	fmt.Println("No saved state found, compiling from source...")
	compiler := compileFromSource()

	// Save the compiled state for next time
	if err := saveCompilerState(compiler, stateFile); err != nil {
		fmt.Printf("Warning: Failed to save state: %v\n", err)
	} else {
		fmt.Printf("✓ Saved compiler state to %s\n", stateFile)
	}

	demonstrateCompiler(compiler)
}

func compileFromSource() *ast.Compiler {
	start := time.Now()

	// Parse all modules
	modules := make(map[string]*ast.Module)
	for name, policy := range examplePolicies {
		module, err := ast.ParseModule(name, policy)
		if err != nil {
			panic(fmt.Sprintf("Failed to parse %s: %v", name, err))
		}
		modules[name] = module
	}

	// Compile
	compiler := ast.NewCompiler()
	compiler.Compile(modules)

	if compiler.Failed() {
		panic(fmt.Sprintf("Compilation failed: %v", compiler.Errors))
	}

	elapsed := time.Since(start)
	fmt.Printf("✓ Compiled %d modules in %v\n", len(modules), elapsed)

	return compiler
}

func saveCompilerState(compiler *ast.Compiler, filename string) error {
	start := time.Now()

	data, err := compiler.MarshalState(version.Version)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	elapsed := time.Since(start)
	fmt.Printf("  Serialized %d bytes in %v\n", len(data), elapsed)

	return nil
}

func loadFromSerializedState(data []byte) (*ast.Compiler, error) {
	start := time.Now()

	// Verify compatibility first
	state, err := ast.UnmarshalState(data)
	if err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	caps := ast.CapabilitiesForThisVersion()
	if err := ast.VerifyStateCompatibility(state, version.Version, caps); err != nil {
		return nil, fmt.Errorf("compatibility check: %w", err)
	}

	// Load the compiler
	compiler, err := ast.NewCompilerFromStateWithOpts(data,
		func(c *ast.Compiler) *ast.Compiler {
			return c.WithCapabilities(caps)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	elapsed := time.Since(start)
	fmt.Printf("  Loaded %d modules in %v\n", len(compiler.Modules), elapsed)

	return compiler, nil
}

func demonstrateCompiler(compiler *ast.Compiler) {
	fmt.Println("\n=== Compiler State ===")
	fmt.Printf("Modules: %d\n", len(compiler.Modules))
	fmt.Printf("Module tree: %v\n", compiler.ModuleTree != nil)
	fmt.Printf("Rule tree: %v\n", compiler.RuleTree != nil)
	fmt.Printf("Dependency graph: %v\n", compiler.Graph != nil)

	// Show some rules
	fmt.Println("\n=== Sample Rules ===")
	showRules(compiler, "data.authz.allow", "Authorization")
	showRules(compiler, "data.rbac.allow", "RBAC")
	showRules(compiler, "data.audit.requires_audit", "Audit")

	// Show required capabilities
	fmt.Println("\n=== Required Capabilities ===")
	if compiler.Required != nil {
		fmt.Printf("Builtins: %d\n", len(compiler.Required.Builtins))
		for _, bi := range compiler.Required.Builtins[:min(5, len(compiler.Required.Builtins))] {
			fmt.Printf("  - %s\n", bi.Name)
		}
		if len(compiler.Required.Builtins) > 5 {
			fmt.Printf("  ... and %d more\n", len(compiler.Required.Builtins)-5)
		}
		if len(compiler.Required.Features) > 0 {
			fmt.Printf("Features: %v\n", compiler.Required.Features)
		}
	}

	// Demonstrate query compilation
	fmt.Println("\n=== Query Compilation ===")
	qc := compiler.QueryCompiler()
	query, err := qc.Compile(ast.MustParseBody("data.authz.allow"))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Compiled query: %v\n", query)
	}
}

func showRules(compiler *ast.Compiler, refStr, category string) {
	ref := ast.MustParseRef(refStr)
	rules := compiler.GetRulesExact(ref)
	fmt.Printf("%s rules (%s): %d\n", category, refStr, len(rules))
	if len(rules) > 0 {
		for i, rule := range rules[:min(3, len(rules))] {
			fmt.Printf("  %d. %v\n", i+1, rule.Head.Ref())
		}
		if len(rules) > 3 {
			fmt.Printf("  ... and %d more\n", len(rules)-3)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Additional utility: Print state info
func printStateInfo(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	state, err := ast.UnmarshalState(data)
	if err != nil {
		return err
	}

	fmt.Printf("\n=== State File Information ===\n")
	fmt.Printf("File: %s\n", filename)
	fmt.Printf("Size: %d bytes (%.2f KB)\n", len(data), float64(len(data))/1024)
	fmt.Printf("Compiler version: %s\n", state.CompilerVersion)
	fmt.Printf("Default Rego version: %v\n", state.DefaultRegoVersion)
	fmt.Printf("Modules: %d\n", len(state.Modules))
	fmt.Printf("Rewritten vars: %d\n", len(state.RewrittenVars))

	// Pretty print a sample
	var prettyJSON map[string]interface{}
	if err := json.Unmarshal(data, &prettyJSON); err == nil {
		if modules, ok := prettyJSON["modules"].(map[string]interface{}); ok {
			fmt.Printf("\nModule names:\n")
			for name := range modules {
				fmt.Printf("  - %s\n", name)
			}
		}
	}

	return nil
}
