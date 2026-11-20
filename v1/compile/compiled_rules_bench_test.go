// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package compile

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/util/test"
)

func BenchmarkBuildCompiledRules(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			var src strings.Builder
			src.WriteString("package authz\n")
			for i := range n {
				fmt.Fprintf(&src, "\n# METADATA\n# labels:\n#   id: \"rule-%d\"\nallow if {\n\tinput.user == \"user-%d\"\n\tstartswith(input.path, \"/%d/\")\n}\n", i, i, i)
			}
			files := map[string]string{
				".manifest":  `{"generated": true, "compiled": true}`,
				"authz.rego": src.String(),
			}
			test.WithTempFS(files, func(root string) {
				b.ReportAllocs()
				for b.Loop() {
					err := New().WithRegoVersion(ast.RegoV1).WithPaths(root).WithAsBundle(true).
						WithOutput(bytes.NewBuffer(nil)).Build(b.Context())
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
