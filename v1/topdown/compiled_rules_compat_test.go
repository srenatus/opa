// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/open-policy-agent/opa/internal/compiledrules"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/test/cases"
	"github.com/open-policy-agent/opa/v1/version"
)

var updateCompiledRules = flag.Bool("update-compiled-rules", false, "write "+compiledRulesFile+" from the v1 test cases")

const compiledRulesFile = "testdata/compiled_rules.json.gz"

// compiledRulesCase is the encoding of a test case's rule sets, for the modules
// with the given hash.
type compiledRulesCase struct {
	Modules string `json:"modules"`
	Rules   []byte `json:"rules"`
}

// TestCompiledRulesFromEarlierOPA runs the v1 test cases with their rule sets
// decoded from compiledRulesFile, written by an earlier OPA. Cases whose modules
// changed since are skipped; `make update-compiled-rules` rewrites the file. If
// this fails, this OPA can't evaluate rules an earlier one compiled: bump the
// format version in internal/compiledrules before rewriting it.
func TestCompiledRulesFromEarlierOPA(t *testing.T) {
	stored := map[string]compiledRulesCase{}
	if !*updateCompiledRules {
		f, err := os.Open(compiledRulesFile)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		r, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewDecoder(r).Decode(&stored); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	written := map[string]compiledRulesCase{}
	var matched atomic.Int64
	t.Cleanup(func() {
		if !*updateCompiledRules {
			// Test cases change, so fewer of them find their encoding over time.
			if n := matched.Load(); n < int64(len(stored)/2) {
				t.Errorf("only %d of %d test cases still have their modules: run make update-compiled-rules", n, len(stored))
			}
			return
		}
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if err := json.NewEncoder(w).Encode(written); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(compiledRulesFile, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	const dir = "../test/cases/testdata/v1"
	seen := map[string]int{}
	for _, tc := range cases.MustLoad(dir).Sorted().Cases {
		// Notes aren't unique, not even within a file.
		key := strings.TrimPrefix(filepath.ToSlash(tc.Filename), dir+"/") + ": " + tc.Note
		if seen[key]++; seen[key] > 1 {
			key += fmt.Sprintf(" (%d)", seen[key])
		}
		t.Run("v1/"+tc.Note, func(t *testing.T) {
			t.Parallel()

			testRun(t, tc, regoVersion(ast.RegoV1), func(o *testOpts) {
				o.compile = func(t *testing.T, modules map[string]string, opts ast.CompileOpts) *ast.Compiler {
					hash := modulesHash(modules)
					raw := make(map[string][]byte, len(modules))
					for name, src := range modules {
						raw[name] = []byte(src)
					}
					sources := func(file string) []byte { return raw[file] }

					var bs []byte
					if *updateCompiledRules {
						bs = encodeRuleSets(t, ast.MustCompileModulesWithOpts(modules, opts), sources)
						if bs != nil {
							mu.Lock()
							written[key] = compiledRulesCase{Modules: hash, Rules: bs}
							mu.Unlock()
						}
					} else if s, ok := stored[key]; ok && s.Modules == hash {
						bs = s.Rules
						matched.Add(1)
					}
					if bs == nil {
						return ast.MustCompileModulesWithOpts(modules, opts)
					}

					_, sets, err := compiledrules.Read(bs, sources)
					if err != nil {
						t.Fatalf("%v: run make update-compiled-rules", err)
					}
					parsed := make(map[string]*ast.Module, len(modules))
					for name, src := range modules {
						mod, err := ast.ParseModuleWithOpts(name, src, opts.ParserOptions)
						if err != nil {
							t.Fatal(err)
						}
						parsed[name] = mod
					}
					c := ast.NewCompiler().
						WithCapabilities(opts.ParserOptions.Capabilities).
						WithDefaultRegoVersion(opts.ParserOptions.RegoVersion)
					return compileWithSets(t, sets, parsed, c)
				}
			})
		})
	}
}

// encodeRuleSets encodes the rule sets of c that it can summarize, nil if none.
func encodeRuleSets(t *testing.T, c *ast.Compiler, sources compiledrules.Sources) []byte {
	t.Helper()

	var sets []compiledrules.Set
	c.RuleTree.DepthFirst(func(node *ast.TreeNode) bool {
		if len(node.Values) == 0 {
			return false
		}
		summary, err := c.Summarize(node.Values[0].Ref().GroundPrefix())
		if err != nil {
			return false
		}
		sets = append(sets, compiledrules.Set{Summary: summary, Rules: node.Values})
		return false
	})
	if len(sets) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := compiledrules.Write(&buf, compiledrules.Key{Version: version.Version}, sets, sources); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func modulesHash(modules map[string]string) string {
	h := sha256.New()
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write([]byte(modules[name]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
