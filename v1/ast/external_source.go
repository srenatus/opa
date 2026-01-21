// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

import (
	"context"
)

type ExternalRuleSource interface {
	// Refs returns the package refs that this source provides rules for.
	// A source can provide rules for multiple packages.
	Refs() []Ref

	// Init returns an initialized [ExternalRuleIndex]. A `Ref` is provided
	// so we know which package we're preparing if multiple Refs are external.
	Init(context.Context, Ref) (ExternalRuleIndex, error)
}

// ExternalRuleIndex mirrors RuleIndex, but add a [context.Context] parameter.
type ExternalRuleIndex interface {
	Lookup(context.Context, ValueResolver) ([]*Rule, error)
	AllRules(context.Context, ValueResolver) ([]*Rule, error)
}
