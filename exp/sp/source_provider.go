// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package sp (Source Provider) provides interfaces for external rule sources.
package sp

import (
	"github.com/open-policy-agent/opa/v1/ast"
)

// Provider is the interface for obtaining Sources. This is what gets
// registered with the compiler for a specific package reference.
type Provider interface {
	// GetSource returns an [ast.ExternalRuleSource] for the given package reference.
	GetSource(pkgRef ast.Ref) (ast.ExternalRuleSource, error)
}
