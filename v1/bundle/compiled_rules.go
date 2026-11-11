// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package bundle

import (
	"bytes"
	"io"
	"slices"

	"github.com/open-policy-agent/opa/internal/compiledrules"
	"github.com/open-policy-agent/opa/internal/debug"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/util"
	"github.com/open-policy-agent/opa/v1/version"
)

// compileRulesOptions are CompileRules' thresholds, which tests set.
type compileRulesOptions struct {
	capabilities *ast.Capabilities
	debug        debug.Debug
	minRules     int // the fewest rules a rule set is compiled with
}

// CompileRules sets b.CompiledRules to the large rule sets of b's modules,
// compiled: those ast.Compiler.Summarize accepts. OPA of this version or newer
// keeps them out of memory when it activates b, others ignore them. The rules
// keep their locations unless b is generated, with the file names a Writer
// gives modules by default. Capabilities are those to compile with, nil for the
// current ones. Which rule sets it compiles, and why it leaves large ones out,
// goes to debugOut if not nil. Experimental.
func (b *Bundle) CompileRules(capabilities *ast.Capabilities, debugOut io.Writer) error {
	opts := compileRulesOptions{capabilities: capabilities, debug: debug.Discard(), minRules: 10}
	if debugOut != nil {
		opts.debug = debug.New(debugOut)
	}
	return b.compileRules(opts)
}

func (b *Bundle) compileRules(opts compileRulesOptions) error {
	b.CompiledRules = nil
	if len(b.Modules) == 0 {
		return nil
	}
	minRules, dbg := opts.minRules, opts.debug
	if dbg == nil {
		dbg = debug.Discard()
	}

	// Parsed like Reader.Read parses them, so the rules' keys are those it finds:
	// with annotations, see ast.Rule.SourceKey.
	popts := ast.ParserOptions{Capabilities: opts.capabilities, ProcessAnnotation: true}
	if b.Manifest.Generated {
		popts = generatedParserOptions(popts)
	}
	popts.RegoVersion = b.RegoVersion(popts.EffectiveRegoVersion())
	parsed := slices.Clone(b.Modules)
	for i, mf := range parsed {
		if mf.URL != "" {
			parsed[i].Path = util.WithPrefix(mf.URL, "/")
		}
	}
	if err := parseModules(parsed, popts, b, parseModule); err != nil {
		return err
	}
	modules := make(map[string]*ast.Module, len(parsed))
	for _, mf := range parsed {
		modules[mf.Path] = mf.Parsed
	}

	// With print statements, so that an OPA keeping them can use the rules too.
	c := ast.NewCompiler().WithCapabilities(opts.capabilities).WithEnablePrintStatements(true)
	if c.Compile(modules); c.Failed() {
		return c.Errors
	}

	// The keys a reader finds in the source, as it leaves the compiled rules out.
	stated := map[ast.RuleKey]struct{}{}
	for _, mf := range parsed {
		for _, key := range compiledrules.KeyedStatements(mf.Raw, true) {
			stated[key] = struct{}{}
		}
	}

	var sets []compiledrules.Set
	c.RuleTree.DepthFirst(func(node *ast.TreeNode) bool {
		if len(node.Values) < minRules {
			return false
		}
		path := node.Values[0].Ref().GroundPrefix()
		s, err := c.Summarize(path)
		if err != nil {
			dbg.Printf("compiled rules: leaving %v (%d rules) out: %v", path, len(node.Values), err)
			return false
		}
		keys := make([]ast.RuleKey, len(node.Values))
		for i, r := range node.Values {
			var ok bool
			if keys[i], ok = r.SourceKey(); !ok {
				dbg.Printf("compiled rules: leaving %v (%d rules) out: rule %d has no source key", path, len(node.Values), i)
				return false
			}
			// Like a function without a body, whose location is only its name.
			if _, ok := stated[keys[i]]; !ok {
				dbg.Printf("compiled rules: leaving %v (%d rules) out: rule %d isn't a statement of its own", path, len(node.Values), i)
				return false
			}
		}
		dbg.Printf("compiled rules: compiling %v (%d rules)", path, len(node.Values))
		sets = append(sets, compiledrules.Set{Summary: s, Rules: node.Values, Keys: keys})
		return false
	})
	if len(sets) == 0 {
		return nil
	}

	files := make(map[*ast.Module]string, len(c.Modules))
	for file, m := range c.Modules {
		files[m] = file
	}
	key := compiledrules.Key{Version: version.Version, Modules: map[string]string{}}
	sources := moduleSources(parsed)
	for _, s := range sets {
		for _, r := range s.Rules {
			if file, ok := files[r.Module]; ok && key.Modules[file] == "" {
				key.Modules[file] = compiledrules.SourceHash(sources(file))
			}
		}
	}
	var buf bytes.Buffer
	if err := compiledrules.Write(&buf, key, sets, sources); err != nil {
		return err
	}
	b.CompiledRules = buf.Bytes()
	return nil
}

// moduleSources gives the raw source of each module by its path, which is the
// file of the locations of its rules.
func moduleSources(modules []ModuleFile) compiledrules.Sources {
	src := make(map[string][]byte, len(modules))
	for _, mf := range modules {
		src[mf.Path] = mf.Raw
	}
	return func(file string) []byte { return src[file] }
}
