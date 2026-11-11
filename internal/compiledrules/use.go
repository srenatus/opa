// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package compiledrules

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"path"
	"sync"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/version"
)

// ParseFunc parses the module at path from its source.
type ParseFunc func(path string, raw []byte, opts ast.ParserOptions) (*ast.Module, error)

// Use is the use of compiled rule sets while parsing the policies they were
// compiled from: which rules to leave out, and which were.
type Use struct {
	Sets     []*ast.LazyRuleSet
	expected map[ast.RuleKey]int
	mu       sync.Mutex
	left     map[ast.RuleKey]int
}

// Usable returns the use of the compiled rule sets bs encodes, or why they can't
// be used: an OPA newer than this one compiled them, or a module they come from
// differs from sources.
func Usable(bs []byte, sources Sources) (*Use, error) {
	key, err := ReadKey(bs)
	if err != nil {
		return nil, err
	}
	if !key.Usable(version.Version) {
		return nil, fmt.Errorf("compiled rules: compiled by OPA %s, newer than this one (%s)", key.Version, version.Version)
	}
	if len(key.Modules) == 0 {
		return nil, errors.New("compiled rules: they don't record the modules they were compiled from")
	}
	for file, hash := range key.Modules {
		if src := sources(file); src == nil || SourceHash(src) != hash {
			return nil, fmt.Errorf("compiled rules: %s isn't the module they were compiled from", file)
		}
	}
	_, sets, err := Read(bs, sources)
	if err != nil {
		return nil, err
	}
	if u := newUse(sets); u != nil {
		return u, nil
	}
	return nil, errors.New("compiled rules: a rule set has no keys of its rules")
}

// newUse returns nil if a set has no keys of its rules.
func newUse(sets []*ast.LazyRuleSet) *Use {
	n := 0
	for _, s := range sets {
		n += len(Keys(s))
	}
	u := &Use{Sets: sets, expected: make(map[ast.RuleKey]int, n), left: make(map[ast.RuleKey]int, n)}
	for _, s := range sets {
		keys := Keys(s)
		if len(keys) == 0 {
			return nil
		}
		for _, k := range keys {
			u.expected[k]++
		}
	}
	return u
}

// PolicyID is the id a bundle's module at path has in the store.
func PolicyID(bundleName, modulePath string) string {
	// Bundle names are sometimes just file paths, some of which
	// are full urls (file:///foo/). Parse these and only use the path.
	parsed, err := url.Parse(bundleName)
	if err == nil {
		return path.Join(parsed.Host, parsed.Path, modulePath)
	}

	return path.Join(bundleName, modulePath)
}

// storedSources gives the sources of a bundle's policies in the store. Read asks
// for every source an encoding refers to, so what it remembers serves decoding
// after txn ends.
func storedSources(ctx context.Context, store storage.Store, txn storage.Transaction, bundleName string) Sources {
	seen := map[string][]byte{}
	return func(file string) []byte {
		if bs, ok := seen[file]; ok {
			return bs
		}
		bs, err := store.GetPolicy(ctx, txn, PolicyID(bundleName, file))
		if err != nil {
			return nil
		}
		seen[file] = bs
		return bs
	}
}

// FromStore returns the use of the compiled rule sets the store keeps for
// bundles other than those named, nil if there are none.
func FromStore(ctx context.Context, store storage.Store, txn storage.Transaction, except map[string]struct{}) *Use {
	cs, ok := store.(storage.CompiledRules)
	if !ok {
		return nil
	}
	kept, err := cs.GetCompiledRules(ctx, txn)
	if err != nil {
		return nil
	}
	var sets []*ast.LazyRuleSet
	for name, bs := range kept {
		if _, ok := except[name]; ok {
			continue
		}
		// A store's compiled rules were read and checked when they were written.
		_, s, err := Read(bs, storedSources(ctx, store, txn, name))
		if err != nil {
			return nil
		}
		sets = append(sets, s...)
	}
	if len(sets) == 0 {
		return nil
	}
	return newUse(sets)
}

// Parser leaves the compiled rule sets' rules out of what parse parses.
func (u *Use) Parser(parse ParseFunc) ParseFunc {
	return func(path string, raw []byte, opts ast.ParserOptions) (*ast.Module, error) {
		var out []byte
		var left []ast.RuleKey
		copied := 0
		// Compiled rule sets key their rules with annotations, however the
		// modules are parsed.
		for s, key := range KeyedStatements(raw, true) {
			if u.expected[key] == 0 {
				continue
			}
			left = append(left, key)
			if out == nil {
				out = make([]byte, 0, len(raw))
			}
			// Newlines in place of skipped lines keep the parsed rules' locations.
			out = append(out, raw[copied:s.annotation]...)
			for range bytes.Count(raw[s.annotation:s.end], []byte("\n")) {
				out = append(out, '\n')
			}
			copied = s.end
		}
		if out == nil {
			return parse(path, raw, opts)
		}
		out = append(out, raw[copied:]...)
		mod, err := parse(path, out, opts)
		if err != nil {
			return parse(path, raw, opts)
		}
		u.mu.Lock()
		defer u.mu.Unlock()
		for _, k := range left {
			u.left[k]++
		}
		return mod, nil
	}
}

// Complete reports whether exactly the compiled rule sets' rules were left out.
func (u *Use) Complete() bool {
	return maps.Equal(u.expected, u.left)
}
