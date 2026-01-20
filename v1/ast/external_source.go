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

	Init(context.Context) (ExternalRuleIndex, error)
}

type ExternalRuleIndex interface {
	// Lookup returns rules optimized for a specific lookup context. The
	// resolver parameter provides access to variable bindings and evaluation
	// state, allowing advanced filtering when beneficial.
	Lookup(ctx context.Context, input *Term, resolver ValueResolver) ([]*Rule, error)

	// AllRules returns all rules for the package. The input parameter allows
	// filtering to return only input-relevant rules.
	AllRules(ctx context.Context, input *Term) ([]*Rule, error)
}
