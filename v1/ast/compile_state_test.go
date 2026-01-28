// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"encoding/json"
	"testing"
)

func TestCompilerStateSerialization(t *testing.T) {
	// Create a simple test module
	module := `package test

p if {
	input.x == 1
}

q contains x if {
	x := input.items[_]
	x > 10
}

r := {"a": 1, "b": 2}
`

	// Compile the module
	compiler := NewCompiler()
	modules := map[string]*Module{
		"test.rego": MustParseModule(module),
	}
	compiler.Compile(modules)

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	// Serialize the compiler state
	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Verify we can unmarshal
	state, err := UnmarshalState(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal state: %v", err)
	}

	if state.CompilerVersion != "test-version" {
		t.Errorf("Expected version 'test-version', got %s", state.CompilerVersion)
	}

	if len(state.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(state.Modules))
	}

	// Create a new compiler from the serialized state
	newCompiler := NewCompiler()
	if err := newCompiler.LoadFromState(state); err != nil {
		t.Fatalf("Failed to load from state: %v", err)
	}

	if newCompiler.Failed() {
		t.Fatalf("Loaded compiler has errors: %v", newCompiler.Errors)
	}

	// Verify the loaded compiler has the same modules
	if len(newCompiler.Modules) != len(compiler.Modules) {
		t.Errorf("Module count mismatch: expected %d, got %d",
			len(compiler.Modules), len(newCompiler.Modules))
	}

	// Verify the module tree was reconstructed
	if newCompiler.ModuleTree == nil {
		t.Error("ModuleTree was not reconstructed")
	}

	// Verify the rule tree was reconstructed
	if newCompiler.RuleTree == nil {
		t.Error("RuleTree was not reconstructed")
	}

	// Verify the graph was reconstructed
	if newCompiler.Graph == nil {
		t.Error("Graph was not reconstructed")
	}

	// Verify we can get rules from the loaded compiler
	rules := newCompiler.GetRulesExact(MustParseRef("data.test.p"))
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule for data.test.p, got %d", len(rules))
	}

	rules = newCompiler.GetRulesExact(MustParseRef("data.test.q"))
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule for data.test.q, got %d", len(rules))
	}

	rules = newCompiler.GetRulesExact(MustParseRef("data.test.r"))
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule for data.test.r, got %d", len(rules))
	}
}

func TestCompilerStateSerializationWithMultipleModules(t *testing.T) {
	modules := map[string]*Module{
		"module1.rego": MustParseModule(`package a.b

p if { input.x > 0 }
`),
		"module2.rego": MustParseModule(`package a.c

q if { data.a.b.p }
`),
		"module3.rego": MustParseModule(`package d

r := 42
`),
	}

	compiler := NewCompiler()
	compiler.Compile(modules)

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	// Serialize and reload
	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	newCompiler, err := NewCompilerFromState(data)
	if err != nil {
		t.Fatalf("Failed to create compiler from state: %v", err)
	}

	// Verify all modules are present
	if len(newCompiler.Modules) != 3 {
		t.Errorf("Expected 3 modules, got %d", len(newCompiler.Modules))
	}

	// Verify cross-module references work
	rules := newCompiler.GetRulesExact(MustParseRef("data.a.c.q"))
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule for data.a.c.q, got %d", len(rules))
	}
}

func TestCompilerStateRewrittenVars(t *testing.T) {
	module := MustParseModule(`package test

p if {
	x := 1  # Will be rewritten
}
`)

	compiler := NewCompiler()
	compiler.Compile(map[string]*Module{"test.rego": module})

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	// Serialize and reload
	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	state, err := UnmarshalState(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal state: %v", err)
	}

	// Verify rewritten vars are preserved
	if len(compiler.RewrittenVars) > 0 && len(state.RewrittenVars) == 0 {
		t.Error("RewrittenVars were not serialized")
	}

	newCompiler, err := NewCompilerFromState(data)
	if err != nil {
		t.Fatalf("Failed to create compiler from state: %v", err)
	}

	// Verify rewritten vars were restored
	if len(newCompiler.RewrittenVars) != len(compiler.RewrittenVars) {
		t.Errorf("RewrittenVars count mismatch: expected %d, got %d",
			len(compiler.RewrittenVars), len(newCompiler.RewrittenVars))
	}
}

func TestCompilerStateCapabilities(t *testing.T) {
	module := MustParseModule(`package test

p if {
	count([1, 2, 3]) == 3
}
`)

	compiler := NewCompiler()
	compiler.Compile(map[string]*Module{"test.rego": module})

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	// Serialize and reload
	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	state, err := UnmarshalState(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal state: %v", err)
	}

	// Verify required capabilities are preserved
	if state.Required == nil {
		t.Error("Required capabilities were not serialized")
	}

	// count builtin should be in the required builtins
	foundCount := false
	for _, bi := range state.Required.Builtins {
		if bi.Name == "count" {
			foundCount = true
			break
		}
	}
	if !foundCount {
		t.Error("Expected 'count' builtin in required capabilities")
	}
}

func TestVerifyStateCompatibility(t *testing.T) {
	tests := []struct {
		name           string
		stateVersion   string
		currentVersion string
		capabilities   *Capabilities
		wantErr        bool
	}{
		{
			name:           "matching versions",
			stateVersion:   "1.0.0",
			currentVersion: "1.0.0",
			capabilities:   nil,
			wantErr:        false,
		},
		{
			name:           "mismatched versions",
			stateVersion:   "1.0.0",
			currentVersion: "2.0.0",
			capabilities:   nil,
			wantErr:        true,
		},
		{
			name:           "empty current version skips check",
			stateVersion:   "1.0.0",
			currentVersion: "",
			capabilities:   nil,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &SerializedCompilerState{
				CompilerVersion: tt.stateVersion,
				Modules:         map[string]*Module{},
			}

			err := VerifyStateCompatibility(state, tt.currentVersion, tt.capabilities)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyStateCompatibility() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCompilerStateJSON(t *testing.T) {
	module := MustParseModule(`package test
p if { true }
`)

	compiler := NewCompiler()
	compiler.Compile(map[string]*Module{"test.rego": module})

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	// Serialize to JSON
	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Verify it's valid JSON
	var rawState map[string]interface{}
	if err := json.Unmarshal(data, &rawState); err != nil {
		t.Fatalf("Serialized data is not valid JSON: %v", err)
	}

	// Check expected fields
	if _, ok := rawState["modules"]; !ok {
		t.Error("Expected 'modules' field in JSON")
	}
	if _, ok := rawState["compiler_version"]; !ok {
		t.Error("Expected 'compiler_version' field in JSON")
	}
	if _, ok := rawState["required_capabilities"]; !ok {
		t.Error("Expected 'required_capabilities' field in JSON")
	}
}

func TestCompilerStateWithOptions(t *testing.T) {
	module := MustParseModule(`package test
p if { input.x > 0 }
`)

	compiler := NewCompiler()
	compiler.Compile(map[string]*Module{"test.rego": module})

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Load with options
	caps := CapabilitiesForThisVersion()
	newCompiler, err := NewCompilerFromStateWithOpts(data,
		func(c *Compiler) *Compiler {
			return c.WithCapabilities(caps)
		},
	)
	if err != nil {
		t.Fatalf("Failed to create compiler from state with opts: %v", err)
	}

	if newCompiler.capabilities == nil {
		t.Error("Expected capabilities to be set")
	}
}

func TestCompilerStateFailsWithErrors(t *testing.T) {
	// Create a compiler with errors
	compiler := NewCompiler()
	compiler.Errors = append(compiler.Errors, NewError(CompileErr, nil, "test error"))

	// Should fail to serialize
	_, err := compiler.MarshalState("test-version")
	if err == nil {
		t.Error("Expected error when serializing failed compiler")
	}
}

func TestCompilerStateIndexReconstruction(t *testing.T) {
	module := MustParseModule(`package test

p contains x if {
	x := input.items[_]
	x > 10
}
`)

	compiler := NewCompiler()
	compiler.Compile(map[string]*Module{"test.rego": module})

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	// Verify indices were built
	if compiler.ruleIndices == nil {
		t.Error("Expected ruleIndices to be built")
	}

	// Serialize and reload
	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	newCompiler, err := NewCompilerFromState(data)
	if err != nil {
		t.Fatalf("Failed to create compiler from state: %v", err)
	}

	// Verify indices were rebuilt
	if newCompiler.ruleIndices == nil {
		t.Error("Expected ruleIndices to be rebuilt")
	}

	// Verify we can get rule index
	ref := MustParseRef("data.test.p")
	idx := newCompiler.RuleIndex(ref)
	if idx == nil {
		t.Error("Expected rule index to exist for data.test.p")
	}
}

func TestCompilerStateDefaultRegoVersion(t *testing.T) {
	module := MustParseModule(`package test
p if { true }
`)

	compiler := NewCompiler()
	compiler.WithDefaultRegoVersion(RegoV1)
	compiler.Compile(map[string]*Module{"test.rego": module})

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	state, err := UnmarshalState(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal state: %v", err)
	}

	if state.DefaultRegoVersion != RegoV1 {
		t.Errorf("Expected DefaultRegoVersion to be RegoV1, got %v", state.DefaultRegoVersion)
	}

	newCompiler, err := NewCompilerFromState(data)
	if err != nil {
		t.Fatalf("Failed to create compiler from state: %v", err)
	}

	if newCompiler.defaultRegoVersion != RegoV1 {
		t.Errorf("Expected loaded compiler DefaultRegoVersion to be RegoV1, got %v", newCompiler.defaultRegoVersion)
	}
}

func TestCompilerStateEvalOnly(t *testing.T) {
	modules := map[string]*Module{
		"policy.rego": MustParseModule(`package test

p if { input.x > 0 }
q if { data.test.p }
`),
	}

	compiler := NewCompiler()
	compiler.Compile(modules)

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Load with eval-only mode
	evalCompiler, err := NewCompilerFromStateForEval(data)
	if err != nil {
		t.Fatalf("Failed to create eval-only compiler: %v", err)
	}

	// Verify eval-only compiler works
	if len(evalCompiler.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(evalCompiler.Modules))
	}

	// Verify essential structures are present
	if evalCompiler.RuleTree == nil {
		t.Error("RuleTree should be built for eval")
	}

	// Verify we can get rules
	rules := evalCompiler.GetRulesExact(MustParseRef("data.test.p"))
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(rules))
	}

	// Verify rule index was built
	if evalCompiler.ruleIndices == nil {
		t.Error("Rule indices should be built for eval")
	}

	// Verify ModuleTree and Graph were skipped (should be nil or empty)
	// Note: These are initialized in NewCompiler, so they won't be nil
	// but they won't have been populated by setModuleTree/setGraph
	// ModuleTree is always initialized in NewCompiler, but in eval-only mode
	// setModuleTree is skipped, so it should only have the root node
	if evalCompiler.ModuleTree != nil {
		hasModules := false
		evalCompiler.ModuleTree.DepthFirst(func(node *ModuleTreeNode) bool {
			if len(node.Modules) > 0 {
				hasModules = true
				return true
			}
			return false
		})
		if hasModules {
			t.Error("ModuleTree should not be populated in eval-only mode")
		}
	}
}

func TestCompilerStateEvalOnlyWithOptions(t *testing.T) {
	module := MustParseModule(`package test
p if { count([1, 2, 3]) == 3 }
`)

	compiler := NewCompiler()
	compiler.Compile(map[string]*Module{"test.rego": module})

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Load with eval-only mode and options
	caps := CapabilitiesForThisVersion()
	evalCompiler, err := NewCompilerFromStateForEvalWithOpts(data,
		func(c *Compiler) *Compiler {
			return c.WithCapabilities(caps)
		},
	)
	if err != nil {
		t.Fatalf("Failed to create eval-only compiler with opts: %v", err)
	}

	if evalCompiler.capabilities == nil {
		t.Error("Expected capabilities to be set")
	}

	// Verify rules are accessible
	rules := evalCompiler.GetRulesExact(MustParseRef("data.test.p"))
	if len(rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(rules))
	}
}

func TestCompilerStateEvalOnlyVsFullComparison(t *testing.T) {
	// Create a more complex policy set for meaningful comparison
	modules := map[string]*Module{
		"authz.rego": MustParseModule(`package authz
import rego.v1

allow if { input.role == "admin" }
allow if { input.role == "user"; input.resource.owner == input.user }
`),
		"rbac.rego": MustParseModule(`package rbac
import rego.v1

roles := {"admin": ["read", "write"], "user": ["read"]}
permissions contains p if { some p in roles[input.role] }
`),
		"audit.rego": MustParseModule(`package audit
import rego.v1

requires_audit if { input.action == "delete" }
requires_audit if { input.role == "admin" }
`),
	}

	compiler := NewCompiler()
	compiler.Compile(modules)

	if compiler.Failed() {
		t.Fatalf("Compilation failed: %v", compiler.Errors)
	}

	data, err := compiler.MarshalState("test-version")
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Load with full mode
	fullCompiler, err := NewCompilerFromState(data)
	if err != nil {
		t.Fatalf("Failed to create full compiler: %v", err)
	}

	// Load with eval-only mode
	evalCompiler, err := NewCompilerFromStateForEval(data)
	if err != nil {
		t.Fatalf("Failed to create eval-only compiler: %v", err)
	}

	// Both should have the same number of modules
	if len(fullCompiler.Modules) != len(evalCompiler.Modules) {
		t.Errorf("Module count mismatch: full=%d, eval=%d",
			len(fullCompiler.Modules), len(evalCompiler.Modules))
	}

	// Both should be able to get the same rules
	testRefs := []string{
		"data.authz.allow",
		"data.rbac.permissions",
		"data.audit.requires_audit",
	}

	for _, refStr := range testRefs {
		ref := MustParseRef(refStr)
		fullRules := fullCompiler.GetRulesExact(ref)
		evalRules := evalCompiler.GetRulesExact(ref)

		if len(fullRules) != len(evalRules) {
			t.Errorf("Rule count mismatch for %s: full=%d, eval=%d",
				refStr, len(fullRules), len(evalRules))
		}
	}

	// Both compilers should have ModuleTree (needed by RuleTree)
	if fullCompiler.ModuleTree == nil {
		t.Error("Full compiler should have ModuleTree")
	}
	if evalCompiler.ModuleTree == nil {
		t.Error("Eval compiler should have ModuleTree")
	}

	// Full compiler should have Graph populated
	if fullCompiler.Graph == nil {
		t.Error("Full compiler should have Graph")
	}

	// Eval compiler may have Graph but it's not populated via setGraph
	// The optimization is that we skip the Graph building step
	t.Log("Eval-only mode skips Graph building for faster loading")
}
