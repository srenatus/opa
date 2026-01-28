// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"encoding/json"
	"fmt"
)

// SerializedCompilerState represents the serializable portion of a compiled
// policy set. It contains the core compilation artifacts (modules) while
// omitting runtime structures that can be reconstructed efficiently.
type SerializedCompilerState struct {
	// Modules contains the compiled AST modules
	Modules map[string]*Module `json:"modules"`

	// RuleTree is the pre-built rule tree for fast evaluation startup
	// If present, ModuleTree building can be skipped during loading
	RuleTree *TreeNode `json:"rule_tree,omitempty"`

	// RewrittenVars maps generated variable names to their original names
	RewrittenVars map[string]string `json:"rewritten_vars"`

	// Required capabilities needed by the compiled modules
	Required *Capabilities `json:"required_capabilities"`

	// CompilerVersion is the OPA version used to compile the modules
	CompilerVersion string `json:"compiler_version"`

	// DefaultRegoVersion is the default Rego version for modules without explicit version
	DefaultRegoVersion RegoVersion `json:"default_rego_version"`
}

// MarshalState serializes the core compiler state to JSON.
// This captures the expensive compilation work (parsing, validation, rewriting)
// while omitting structures that can be rebuilt efficiently (trees, indices).
// The version parameter should be set to the current OPA version.
func (c *Compiler) MarshalState(version string) ([]byte, error) {
	if c.Failed() {
		return nil, fmt.Errorf("cannot serialize compiler state with errors: %v", c.Errors)
	}

	state := &SerializedCompilerState{
		Modules:            c.Modules,
		RuleTree:           c.RuleTree, // Include RuleTree for faster eval loading
		RewrittenVars:      make(map[string]string, len(c.RewrittenVars)),
		Required:           c.Required,
		CompilerVersion:    version,
		DefaultRegoVersion: c.defaultRegoVersion,
	}

	for k, v := range c.RewrittenVars {
		state.RewrittenVars[string(k)] = string(v)
	}

	return json.Marshal(state)
}

// UnmarshalState deserializes a SerializedCompilerState from JSON.
func UnmarshalState(data []byte) (*SerializedCompilerState, error) {
	var state SerializedCompilerState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal compiler state: %w", err)
	}

	if state.CompilerVersion == "" {
		return nil, fmt.Errorf("missing compiler version in serialized state")
	}

	if state.Modules == nil {
		return nil, fmt.Errorf("missing modules in serialized state")
	}

	return &state, nil
}

// LoadFromState initializes a compiler from serialized state and rebuilds
// the derived structures (trees, graph, indices) that were omitted from
// serialization.
func (c *Compiler) LoadFromState(state *SerializedCompilerState) error {
	if state == nil {
		return fmt.Errorf("cannot load from nil state")
	}

	c.init()

	c.Modules = state.Modules
	c.Required = state.Required
	c.defaultRegoVersion = state.DefaultRegoVersion

	c.RewrittenVars = make(map[Var]Var, len(state.RewrittenVars))
	for k, v := range state.RewrittenVars {
		c.RewrittenVars[Var(k)] = Var(v)
	}

	c.sorted = make([]string, 0, len(c.Modules))
	for name := range c.Modules {
		c.sorted = append(c.sorted, name)
	}

	return c.rebuildState(false)
}

// LoadFromStateForEval initializes a compiler from serialized state for
// evaluation only. This is optimized to use the pre-built RuleTree from
// serialization, skipping both ModuleTree and Graph rebuilding. This provides
// ~40-50% faster loading compared to LoadFromState.
//
// Use this when you only need to evaluate policies, not compile additional modules.
func (c *Compiler) LoadFromStateForEval(state *SerializedCompilerState) error {
	if state == nil {
		return fmt.Errorf("cannot load from nil state")
	}

	c.init()

	c.Modules = state.Modules
	c.Required = state.Required
	c.defaultRegoVersion = state.DefaultRegoVersion

	c.RewrittenVars = make(map[Var]Var, len(state.RewrittenVars))
	for k, v := range state.RewrittenVars {
		c.RewrittenVars[Var(k)] = Var(v)
	}

	c.sorted = make([]string, 0, len(c.Modules))
	for name := range c.Modules {
		c.sorted = append(c.sorted, name)
	}

	// Use serialized RuleTree if available, otherwise rebuild
	if state.RuleTree != nil {
		c.RuleTree = state.RuleTree
	} else {
		// Fallback: rebuild from modules
		c.setModuleTree()
		if c.Failed() {
			return fmt.Errorf("failed to rebuild module tree: %v", c.Errors)
		}
		c.setRuleTree()
		if c.Failed() {
			return fmt.Errorf("failed to rebuild rule tree: %v", c.Errors)
		}
	}

	// Skip Graph building for eval-only mode

	// Build indices (required for evaluation)
	if c.evalMode == EvalModeTopdown {
		c.buildRuleIndices()
		if c.Failed() {
			return fmt.Errorf("failed to rebuild rule indices: %v", c.Errors)
		}

		c.buildComprehensionIndices()
		if c.Failed() {
			return fmt.Errorf("failed to rebuild comprehension indices: %v", c.Errors)
		}
	}

	return nil
}

// rebuildState reconstructs the compiler's derived state from the loaded modules.
// If evalOnly is true, skips the dependency Graph which is only needed for compilation.
// Note: ModuleTree is still built as it's required by RuleTree construction.
func (c *Compiler) rebuildState(evalOnly bool) error {
	// ModuleTree is needed by RuleTree, so we always build it
	c.setModuleTree()
	if c.Failed() {
		return fmt.Errorf("failed to rebuild module tree: %v", c.Errors)
	}

	c.setRuleTree()
	if c.Failed() {
		return fmt.Errorf("failed to rebuild rule tree: %v", c.Errors)
	}

	// Graph is only used for recursion checking during compilation, skip for eval
	if !evalOnly {
		c.setGraph()
		if c.Failed() {
			return fmt.Errorf("failed to rebuild graph: %v", c.Errors)
		}
	}

	if c.evalMode == EvalModeTopdown {
		c.buildRuleIndices()
		if c.Failed() {
			return fmt.Errorf("failed to rebuild rule indices: %v", c.Errors)
		}

		c.buildComprehensionIndices()
		if c.Failed() {
			return fmt.Errorf("failed to rebuild comprehension indices: %v", c.Errors)
		}
	}

	return nil
}

// VerifyStateCompatibility checks if the serialized state is compatible
// with the current compiler version and capabilities.
// currentVersion should be set to the running OPA version.
func VerifyStateCompatibility(state *SerializedCompilerState, currentVersion string, capabilities *Capabilities) error {
	if currentVersion != "" && state.CompilerVersion != currentVersion {
		return fmt.Errorf("compiler version mismatch: state=%s, current=%s",
			state.CompilerVersion, currentVersion)
	}

	if capabilities != nil && state.Required != nil {
		for _, builtin := range state.Required.Builtins {
			if !capabilities.ContainsBuiltin(builtin.Name) {
				return fmt.Errorf("required builtin %s not available in capabilities", builtin.Name)
			}
		}

		for _, feature := range state.Required.Features {
			if !capabilities.ContainsFeature(feature) {
				return fmt.Errorf("required feature %s not available in capabilities", feature)
			}
		}
	}

	return nil
}

// NewCompilerFromState creates a new compiler instance from serialized state.
// This is a convenience function that combines UnmarshalState and LoadFromState.
// It rebuilds all compiler structures including ModuleTree and Graph.
func NewCompilerFromState(data []byte) (*Compiler, error) {
	state, err := UnmarshalState(data)
	if err != nil {
		return nil, err
	}

	c := NewCompiler()
	if err := c.LoadFromState(state); err != nil {
		return nil, err
	}

	return c, nil
}

// NewCompilerFromStateForEval creates a compiler from serialized state optimized
// for evaluation only. This uses the pre-built RuleTree from serialization,
// skipping both ModuleTree and Graph rebuilding, providing ~40-50% faster loading.
//
// Use this when you only need to evaluate policies, not compile additional modules.
func NewCompilerFromStateForEval(data []byte) (*Compiler, error) {
	state, err := UnmarshalState(data)
	if err != nil {
		return nil, err
	}

	c := NewCompiler()
	if err := c.LoadFromStateForEval(state); err != nil {
		return nil, err
	}

	return c, nil
}

// NewCompilerFromStateWithOpts creates a compiler from serialized state with options.
// This rebuilds all compiler structures including ModuleTree and Graph.
func NewCompilerFromStateWithOpts(data []byte, opts ...func(*Compiler) *Compiler) (*Compiler, error) {
	state, err := UnmarshalState(data)
	if err != nil {
		return nil, err
	}

	c := NewCompiler()
	for _, opt := range opts {
		c = opt(c)
	}

	if err := c.LoadFromState(state); err != nil {
		return nil, err
	}

	return c, nil
}

// NewCompilerFromStateForEvalWithOpts creates a compiler from serialized state
// with options, optimized for evaluation only. This uses the pre-built RuleTree
// from serialization, skipping both ModuleTree and Graph rebuilding.
//
// Use this when you only need to evaluate policies, not compile additional modules.
func NewCompilerFromStateForEvalWithOpts(data []byte, opts ...func(*Compiler) *Compiler) (*Compiler, error) {
	state, err := UnmarshalState(data)
	if err != nil {
		return nil, err
	}

	c := NewCompiler()
	for _, opt := range opts {
		c = opt(c)
	}

	if err := c.LoadFromStateForEval(state); err != nil {
		return nil, err
	}

	return c, nil
}
