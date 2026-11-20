// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package bundle

import (
	"fmt"
	"testing"
)

func BenchmarkCompileRules(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			src := generatedBundle(n)
			b.ReportAllocs()
			for b.Loop() {
				bundle := *src
				if err := bundle.compileRules(compileRulesOptions{minRules: 10}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
