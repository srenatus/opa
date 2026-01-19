// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package ast

// ExternalRuleSource provides rules lazily during evaluation.
type ExternalRuleSource interface {
	// GetRules returns rules for the package. The input parameter allows
	// filtering to return only input-relevant rules.
	GetRules(input *Term) ([]*Rule, error)
}
