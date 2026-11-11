// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package compiledrules encodes compiled rule sets, so that a compiler can read
// their rules on demand rather than holding them; see ast.LazyRuleSet.
//
// An encoding is, with numbers as unsigned varints and strings as indices into
// the string table unless said otherwise:
//
//	magic      "OPACRS\x00" and the format version, one byte
//	key        length, then Key as JSON
//	strings    count, then per string its length and bytes
//	files      count, then per file its name and the length of its source
//	sets       count, then per rule set:
//	  summary  length, then path, head, the type as JSON, the dependencies,
//	           the base documents read and the built-in functions called
//	  located  one byte, 1 if the rules have locations
//	  offsets  rule count n, then n+1 offsets into rules, as uint32 little endian
//	  rules    length, then the rules: rule i is offsets[i] to offsets[i+1]
//	  keys     count, 0 or n, then the 16 bytes of each rule's ast.RuleKey
//
// A rule is its flags (default, else), location, annotations, head, body and
// else branch, expressions and terms following the AST, a term as a tag and its
// value. Each rule decodes on its own: it reads only its bytes and the string
// table. An annotation is its scope, its labels (none, string pairs where all
// values are strings, or a term), its other fields as JSON if it has any, and
// its location.
//
// Locations are encoded as spans of the source of the rules' modules, which
// Write and Read are given; Read refuses sources of other lengths. A located
// rule starts with 1 and its file, offset, row and column, or with 0 if there
// is no source for it. Every location in it is then 0 for none; 1, an offset
// from the rule's and a length, or 3, an offset back from the rule's (like its
// METADATA's) and a length: the text aliases the source, and row, column and
// tabs are derived from it; or 2 and the location in full, where the source
// doesn't give it back exactly. Generated bundles have no locations, so their
// rule sets aren't located.
package compiledrules

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"unicode/utf8"

	"github.com/open-policy-agent/opa/internal/semver"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/types"
	"github.com/open-policy-agent/opa/v1/util"
)

// magic ends in the format version, which changes when what an OPA reads from an
// encoding changes, or when it can't evaluate rules an older OPA compiled
// (TestCompiledRulesFromEarlierOPA in v1/topdown tells).
const magic = "OPACRS\x00\x01"

// Key is what compiled rules were compiled with, which decides whether a
// compiler can use them.
type Key struct {
	// Version is the version of the OPA that compiled them, the oldest that
	// can use them.
	Version string `json:"version"`
	// Modules are the modules their rules come from, by file, each as the
	// SHA-256 of its source in hex: a module that differs, in its imports for
	// one, may compile its rules differently.
	Modules map[string]string `json:"modules,omitempty"`
}

// SourceHash is a module's source as Key.Modules records it.
func SourceHash(src []byte) string {
	h := sha256.Sum256(src)
	return hex.EncodeToString(h[:])
}

// Usable reports whether an OPA of the given version can use rules compiled
// with k.
func (k Key) Usable(version string) bool {
	return semver.Compare(version, k.Version) >= 0
}

// Set is a compiled rule set to encode, with the keys of its rules' source
// texts, if there are any; see ast.LazyRuleSet.
type Set struct {
	Summary *ast.RuleSetSummary
	Rules   []*ast.Rule
	Keys    []ast.RuleKey
}

// Write encodes sets, compiled with key. The locations of rules are encoded as
// spans of the sources' text, which Read has to be given too.
func Write(w io.Writer, key Key, sets []Set, sources Sources) error {
	keyJSON, err := json.Marshal(key)
	if err != nil {
		return err
	}
	e := &encoder{strings: map[string]uint64{}, sources: sources, files: map[string]int{}}

	var body bytes.Buffer
	for _, set := range sets {
		e.located = false // summaries carry no locations
		sb := e.summary(set.Summary)
		body.Write(binary.AppendUvarint(nil, uint64(len(sb))))
		body.Write(sb)

		e.located = len(set.Rules) > 0 && set.Rules[0].Location != nil
		body.WriteByte(boolByte(e.located))

		offsets := make([]byte, 0, 4*(len(set.Rules)+1))
		e.buf = nil
		for _, r := range set.Rules {
			offsets = binary.LittleEndian.AppendUint32(offsets, uint32(len(e.buf)))
			e.rootRule(r)
		}
		if e.err != nil {
			return fmt.Errorf("rule set %v: %w", set.Summary.Path, e.err)
		}
		offsets = binary.LittleEndian.AppendUint32(offsets, uint32(len(e.buf)))
		body.Write(binary.AppendUvarint(nil, uint64(len(set.Rules))))
		body.Write(offsets)
		body.Write(binary.AppendUvarint(nil, uint64(len(e.buf))))
		body.Write(e.buf)
		if len(set.Keys) != 0 && len(set.Keys) != len(set.Rules) {
			return fmt.Errorf("rule set %v: %d keys for %d rules", set.Summary.Path, len(set.Keys), len(set.Rules))
		}
		body.Write(binary.AppendUvarint(nil, uint64(len(set.Keys))))
		for _, k := range set.Keys {
			body.Write(k[:])
		}
	}

	out := append([]byte(magic), binary.AppendUvarint(nil, uint64(len(keyJSON)))...)
	out = append(out, keyJSON...)
	out = binary.AppendUvarint(out, uint64(len(e.table)))
	for _, s := range e.table {
		out = binary.AppendUvarint(out, uint64(len(s)))
		out = append(out, s...)
	}
	out = binary.AppendUvarint(out, uint64(len(e.files)))
	for _, file := range slices.Sorted(maps.Keys(e.files)) {
		out = binary.AppendUvarint(out, uint64(e.strings[file]))
		out = binary.AppendUvarint(out, uint64(e.files[file]))
	}
	out = binary.AppendUvarint(out, uint64(len(sets)))
	if _, err := w.Write(out); err != nil {
		return err
	}
	_, err = w.Write(body.Bytes())
	return err
}

// ReadKey decodes what the rules encoded in bs were compiled with. It fails for
// encodings of another format version.
func ReadKey(bs []byte) (Key, error) {
	d := &decoder{b: bs}
	return d.key()
}

// Read decodes the key and rule sets encoded in bs, which the returned rule
// sets read their rules from: bs and the sources must not change while they
// are in use. It fails if sources don't give the texts the encoding refers to.
func Read(bs []byte, sources Sources) (Key, []*ast.LazyRuleSet, error) {
	d := &decoder{b: bs}
	key, err := d.key()
	if err != nil {
		return key, nil, err
	}
	n := d.uvarint()
	d.strings = make([]string, 0, n)
	for range n {
		d.strings = append(d.strings, d.bytesString())
	}

	for range d.uvarint() {
		file, size := d.str(), d.uvarint()
		if d.err == nil && (sources == nil || len(sources(file)) != size) {
			return key, nil, fmt.Errorf("compiled rules: source of %v differs", file)
		}
	}

	count := d.uvarint()
	sets := make([]*ast.LazyRuleSet, 0, count)
	for range count {
		n := d.uvarint()
		start := d.pos
		summary, err := d.summary()
		if err != nil {
			return key, nil, err
		}
		if d.pos != start+n {
			return key, nil, errors.New("compiled rules: malformed rule set summary")
		}
		located := d.byte() != 0
		rules := int(d.uvarint())
		offsets := d.take(4 * (rules + 1))
		block := d.take(int(d.uvarint()))
		var keys []ast.RuleKey
		if n := d.uvarint(); n > 0 && d.err == nil {
			raw := d.take(n * len(ast.RuleKey{}))
			if n != rules {
				d.fail(errors.New("compiled rules: keys don't match rules"))
			}
			if d.err == nil {
				keys = make([]ast.RuleKey, n)
				for i := range keys {
					copy(keys[i][:], raw[i*len(ast.RuleKey{}):])
				}
			}
		}
		if d.err != nil {
			return key, nil, d.err
		}
		pkg := summary.Path[:len(summary.Path)-len(summary.Head.Ref())]
		sets = append(sets, &ast.LazyRuleSet{
			Summary: summary,
			Rules: &lazyRules{
				located: located,
				sources: sources,
				strings: d.strings,
				offsets: offsets,
				block:   block,
				module:  &ast.Module{Package: &ast.Package{Path: pkg}},
				keys:    keys,
			},
		})
	}
	return key, sets, d.err
}

func (d *decoder) key() (Key, error) {
	var key Key
	if !bytes.HasPrefix(d.b, []byte(magic)) {
		return key, errors.New("compiled rules: not an encoding of compiled rules of this format version")
	}
	d.pos = len(magic)
	bs := d.take(d.uvarint())
	if d.err != nil {
		return key, d.err
	}
	return key, json.Unmarshal(bs, &key)
}

type lazyRules struct {
	located bool
	sources Sources
	strings []string
	offsets []byte
	block   []byte
	module  *ast.Module
	keys    []ast.RuleKey
}

// Keys returns the keys of the source of the rules of a rule set Read returned,
// in the order of its rules, by which a parser can leave them out; see
// ast.Rule.SourceKey. It returns nil if they weren't encoded.
func Keys(set *ast.LazyRuleSet) []ast.RuleKey {
	if l, ok := set.Rules.(*lazyRules); ok {
		return l.keys
	}
	return nil
}

func (l *lazyRules) Len() int { return len(l.offsets)/4 - 1 }

func (l *lazyRules) Rule(i int) (*ast.Rule, error) { return l.rule(i, false) }

// IndexRule leaves out locations and annotations, see ast.LazyRules.
func (l *lazyRules) IndexRule(i int) (*ast.Rule, error) { return l.rule(i, true) }

func (l *lazyRules) rule(i int, bare bool) (*ast.Rule, error) {
	start := binary.LittleEndian.Uint32(l.offsets[4*i:])
	end := binary.LittleEndian.Uint32(l.offsets[4*(i+1):])
	d := &decoder{b: l.block[start:end], strings: l.strings, located: l.located, bare: bare, sources: l.sources}
	r := d.rootRule(l.module)
	return r, d.err
}

// Tags of the values of terms.
const (
	tagNull byte = iota
	tagTrue
	tagFalse
	tagNumber
	tagString
	tagVar
	tagRef
	tagArray
	tagSet
	tagObject
	tagArrayComprehension
	tagSetComprehension
	tagObjectComprehension
	tagCall
)

// Kinds of the terms of expressions.
const (
	exprTerm byte = iota
	exprCall
	exprSomeDecl
	exprEvery
	exprNot
	exprAnd
	exprOr
)

const (
	flagNegated = 1 << iota
	flagGenerated
	flagWith
)

const (
	headAssign = 1 << iota
	headKey
	headValue
)

const (
	ruleDefault = 1 << iota
	ruleElse
)

type encoder struct {
	strings map[string]uint64
	table   []string
	buf     []byte
	err     error

	// located is whether locations are encoded. sources gives the source of a
	// file, and src, file, base, row, col and line describe the rule being
	// encoded: its source, file, offset, row, column, and the offset its line
	// starts at. files records the length of each source used.
	located bool
	sources Sources
	files   map[string]int
	src     []byte
	file    string
	base    int
	row     int
	col     int
	line    int
}

// Sources returns the source text of a file, nil if there is none.
type Sources func(file string) []byte

func (e *encoder) rootRule(r *ast.Rule) {
	if e.located {
		e.src = nil
		if l := r.Location; l != nil && e.sources != nil {
			e.src = e.sources(l.File)
		}
		if e.src == nil || r.Location.Offset > len(e.src) {
			e.src = nil
			e.byte(0)
		} else {
			l := r.Location
			e.file, e.base, e.row, e.col = l.File, l.Offset, l.Row, l.Col
			e.line = bytes.LastIndexByte(e.src[:e.base], '\n') + 1
			e.files[e.file] = len(e.src)
			e.byte(1)
			e.str(e.file)
			e.uvarint(e.base)
			e.uvarint(e.row)
			e.uvarint(e.col)
		}
	}
	e.rule(r)
}

// position derives the row, column and tabs of offset in a rule's source, as
// the scanner counts them: columns in runes, tabs as the columns of the tabs
// on the line before.
func position(src []byte, base, row, line, offset int) (int, int, []int) {
	for i := base; i < offset; i++ {
		if src[i] == '\n' {
			row, line = row+1, i+1
		}
	}
	col := 0
	var tabs []int
	for i := line; i <= offset && i < len(src); {
		r, w := utf8.DecodeRune(src[i:])
		col++
		if r == '\t' {
			tabs = append(tabs, col)
		}
		i += w
	}
	return row, col, tabs
}

// loc encodes a location as a span of the rule's source where the span gives
// it back exactly, and in full otherwise.
func (e *encoder) loc(l *ast.Location) {
	if !e.located {
		return
	}
	if l == nil {
		e.byte(0)
		return
	}
	if e.src != nil && l.File == e.file && l.Offset >= 0 && l.Offset+len(l.Text) <= len(e.src) &&
		bytes.Equal(e.src[l.Offset:l.Offset+len(l.Text)], l.Text) {
		if l.Offset >= e.base {
			if row, col, tabs := position(e.src, e.base, e.row, e.line, l.Offset); row == l.Row && col == l.Col && slices.Equal(tabs, l.Tabs) {
				e.byte(1)
				e.uvarint(l.Offset - e.base)
				e.uvarint(len(l.Text))
				return
			}
		} else {
			// Before the rule, like its METADATA.
			row := e.row - bytes.Count(e.src[l.Offset:e.base], []byte("\n"))
			line := bytes.LastIndexByte(e.src[:l.Offset], '\n') + 1
			if r, col, tabs := position(e.src, l.Offset, row, line, l.Offset); r == l.Row && col == l.Col && slices.Equal(tabs, l.Tabs) {
				e.byte(3)
				e.uvarint(e.base - l.Offset)
				e.uvarint(len(l.Text))
				return
			}
		}
	}
	e.byte(2)
	e.uvarint(len(l.Text))
	e.byte(l.Text...)
	e.uvarint(l.Offset)
	e.str(l.File)
	e.uvarint(l.Row)
	e.uvarint(l.Col)
	e.uvarint(len(l.Tabs))
	for _, t := range l.Tabs {
		e.uvarint(t)
	}
}

func (e *encoder) fail(err error) {
	if e.err == nil {
		e.err = err
	}
}

func (e *encoder) byte(b ...byte) { e.buf = append(e.buf, b...) }

func (e *encoder) uvarint(n int) { e.buf = binary.AppendUvarint(e.buf, uint64(n)) }

func (e *encoder) str(s string) {
	i, ok := e.strings[s]
	if !ok {
		i = uint64(len(e.table))
		e.strings[s] = i
		e.table = append(e.table, s)
	}
	e.buf = binary.AppendUvarint(e.buf, i)
}

func (e *encoder) summary(s *ast.RuleSetSummary) []byte {
	tpe, err := json.Marshal(s.Type)
	e.fail(err)
	e.buf = nil
	e.terms(s.Path)
	e.head(s.Head)
	e.uvarint(len(tpe))
	e.byte(tpe...)
	for _, refs := range [][]ast.Ref{s.Deps, s.DataRefs} {
		e.uvarint(len(refs))
		for _, ref := range refs {
			e.terms(ref)
		}
	}
	e.uvarint(len(s.Builtins))
	for _, b := range s.Builtins {
		e.str(b)
	}
	return e.buf
}

func (e *encoder) rule(r *ast.Rule) {
	var flags byte
	if r.Default {
		flags |= ruleDefault
	}
	if r.Else != nil {
		flags |= ruleElse
	}
	e.byte(flags)
	e.loc(r.Location)
	e.annotations(r.Annotations)
	e.head(r.Head)
	e.body(r.Body)
	if r.Else != nil {
		e.rule(r.Else)
	}
}

// How an annotation's labels are encoded: none, as pairs of strings where all
// values are strings, which decodes without going through a term, or as a term.
const (
	labelsNone byte = iota
	labelsStrings
	labelsTerm
)

func stringValued(labels map[string]any) bool {
	for _, v := range labels {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

// noAnnotationFields is the JSON of annotations without scope, labels and
// location, which are encoded on their own.
const noAnnotationFields = `{"scope":""}`

func (e *encoder) annotations(as []*ast.Annotations) {
	e.uvarint(len(as))
	for _, a := range as {
		e.str(a.Scope)
		switch {
		case len(a.Labels) == 0:
			e.byte(labelsNone)
		case stringValued(a.Labels):
			e.byte(labelsStrings)
			e.uvarint(len(a.Labels))
			for _, k := range slices.Sorted(maps.Keys(a.Labels)) {
				e.str(k)
				e.str(a.Labels[k].(string))
			}
		default:
			labels, err := ast.InterfaceToValue(a.Labels)
			e.fail(err)
			e.byte(labelsTerm)
			e.term(ast.NewTerm(labels))
		}
		rest := *a
		rest.Scope, rest.Labels, rest.Location = "", nil, nil
		bs, err := json.Marshal(&rest)
		e.fail(err)
		if string(bs) == noAnnotationFields {
			bs = nil
		}
		e.uvarint(len(bs))
		e.byte(bs...)
		e.loc(a.Location)
	}
}

func (e *encoder) head(h *ast.Head) {
	var flags byte
	if h.Assign {
		flags |= headAssign
	}
	if h.Key != nil {
		flags |= headKey
	}
	if h.Value != nil {
		flags |= headValue
	}
	e.byte(flags)
	e.loc(h.Location)
	e.str(string(h.Name))
	e.terms(h.Reference)
	e.terms(h.Args)
	for _, t := range []*ast.Term{h.Key, h.Value} {
		if t != nil {
			e.term(t)
		}
	}
}

func (e *encoder) body(b ast.Body) {
	e.uvarint(len(b))
	for _, x := range b {
		e.expr(x)
	}
}

func (e *encoder) expr(x *ast.Expr) {
	var flags byte
	if x.Negated {
		flags |= flagNegated
	}
	if x.Generated {
		flags |= flagGenerated
	}
	if len(x.With) > 0 {
		flags |= flagWith
	}
	e.byte(flags)
	e.uvarint(x.Index)
	e.loc(x.Location)

	switch ts := x.Terms.(type) {
	case *ast.Term:
		e.byte(exprTerm)
		e.term(ts)
	case []*ast.Term:
		e.byte(exprCall)
		e.terms(ts)
	case *ast.SomeDecl:
		e.byte(exprSomeDecl)
		e.loc(ts.Location)
		e.terms(ts.Symbols)
	case *ast.Every:
		e.byte(exprEvery, boolByte(ts.Key != nil))
		e.loc(ts.Location)
		if ts.Key != nil {
			e.term(ts.Key)
		}
		e.term(ts.Value)
		e.term(ts.Domain)
		e.body(ts.Body)
	case *ast.Not:
		e.byte(exprNot, boolByte(ts.ExplicitBody))
		e.loc(ts.Location)
		e.body(ts.Body)
	case *ast.LogicalAnd:
		e.byte(exprAnd, boolByte(ts.ExplicitLhs), boolByte(ts.ExplicitRhs))
		e.loc(ts.Location)
		e.body(ts.Lhs)
		e.body(ts.Rhs)
	case *ast.LogicalOr:
		e.byte(exprOr, boolByte(ts.ExplicitLhs), boolByte(ts.ExplicitRhs))
		e.loc(ts.Location)
		e.body(ts.Lhs)
		e.body(ts.Rhs)
	default:
		e.fail(fmt.Errorf("unsupported expression terms %T", x.Terms))
	}

	if len(x.With) > 0 {
		e.uvarint(len(x.With))
		for _, w := range x.With {
			e.loc(w.Location)
			e.term(w.Target)
			e.term(w.Value)
		}
	}
}

func (e *encoder) terms(ts []*ast.Term) {
	e.uvarint(len(ts))
	for _, t := range ts {
		e.term(t)
	}
}

func (e *encoder) term(t *ast.Term) {
	e.loc(t.Location)
	switch v := t.Value.(type) {
	case ast.Null:
		e.byte(tagNull)
	case ast.Boolean:
		if v {
			e.byte(tagTrue)
		} else {
			e.byte(tagFalse)
		}
	case ast.Number:
		e.byte(tagNumber)
		e.str(string(v))
	case ast.String:
		e.byte(tagString)
		e.str(string(v))
	case ast.Var:
		e.byte(tagVar)
		e.str(string(v))
	case ast.Ref:
		e.byte(tagRef)
		e.terms(v)
	case *ast.Array:
		e.byte(tagArray)
		e.uvarint(v.Len())
		v.Foreach(e.term)
	case ast.Set:
		e.byte(tagSet)
		e.terms(v.Slice())
	case ast.Object:
		e.byte(tagObject)
		e.uvarint(v.Len())
		v.Foreach(func(k, x *ast.Term) {
			e.term(k)
			e.term(x)
		})
	case *ast.ArrayComprehension:
		e.byte(tagArrayComprehension)
		e.term(v.Term)
		e.body(v.Body)
	case *ast.SetComprehension:
		e.byte(tagSetComprehension)
		e.term(v.Term)
		e.body(v.Body)
	case *ast.ObjectComprehension:
		e.byte(tagObjectComprehension)
		e.term(v.Key)
		e.term(v.Value)
		e.body(v.Body)
	case ast.Call:
		e.byte(tagCall)
		e.terms(v)
	default:
		e.fail(fmt.Errorf("unsupported term %T", t.Value))
	}
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

type decoder struct {
	located bool
	// bare skips locations and annotations, for a rule read only to index it.
	bare    bool
	sources Sources
	src     []byte
	file    string
	base    int
	row     int
	col     int
	// at is where positions are derived from: an offset in the rule's source,
	// its row, and the runes and tabs on its line before it.
	at cursor

	b       []byte
	pos     int
	strings []string
	err     error
}

func (d *decoder) fail(err error) {
	if d.err == nil {
		d.err = err
	}
}

func (d *decoder) byte() byte {
	if d.pos >= len(d.b) {
		d.fail(io.ErrUnexpectedEOF)
		return 0
	}
	b := d.b[d.pos]
	d.pos++
	return b
}

func (d *decoder) uvarint() int {
	if d.err != nil {
		return 0
	}
	n, k := binary.Uvarint(d.b[d.pos:])
	if k <= 0 {
		d.fail(io.ErrUnexpectedEOF)
		return 0
	}
	d.pos += k
	return int(n)
}

func (d *decoder) take(n int) []byte {
	if d.err != nil || n < 0 || d.pos+n > len(d.b) {
		d.fail(io.ErrUnexpectedEOF)
		return nil
	}
	b := d.b[d.pos : d.pos+n]
	d.pos += n
	return b
}

// bytesString reads a length-prefixed string, which shares the bytes it is
// read from.
func (d *decoder) bytesString() string {
	return util.ByteSliceToString(d.take(d.uvarint()))
}

func (d *decoder) str() string {
	i := d.uvarint()
	if i >= len(d.strings) {
		d.fail(fmt.Errorf("compiled rules: string %d out of range", i))
		return ""
	}
	return d.strings[i]
}

func (d *decoder) summary() (*ast.RuleSetSummary, error) {
	s := &ast.RuleSetSummary{Path: d.ref(), Head: d.head()}
	tpe := d.take(d.uvarint())
	if d.err != nil {
		return nil, d.err
	}
	var err error
	if s.Type, err = types.Unmarshal(tpe); err != nil {
		return nil, err
	}
	for _, refs := range []*[]ast.Ref{&s.Deps, &s.DataRefs} {
		n := d.uvarint()
		for range n {
			*refs = append(*refs, d.ref())
		}
	}
	n := d.uvarint()
	for range n {
		s.Builtins = append(s.Builtins, d.str())
	}
	return s, d.err
}

func (d *decoder) rootRule(module *ast.Module) *ast.Rule {
	if d.located {
		d.src = nil
		if d.byte() == 1 {
			d.file = d.str()
			d.base, d.row, d.col = d.uvarint(), d.uvarint(), d.uvarint()
			if d.bare {
				return d.rule(module)
			}
			if d.sources != nil {
				d.src = d.sources(d.file)
			}
			if d.src == nil || d.base > len(d.src) {
				d.fail(fmt.Errorf("compiled rules: no source for %v", d.file))
				return nil
			}
			d.at = d.start()
		}
	}
	return d.rule(module)
}

func (d *decoder) loc() *ast.Location {
	if !d.located {
		return nil
	}
	switch mode := d.byte(); mode {
	case 0:
		return nil
	case 1, 3:
		off, n := d.uvarint(), d.uvarint()
		if d.bare {
			return nil
		}
		start := d.base + off
		if mode == 3 {
			start = d.base - off
		}
		if d.err != nil || d.src == nil || start < 0 || start+n > len(d.src) {
			d.fail(io.ErrUnexpectedEOF)
			return nil
		}
		l := &ast.Location{Text: d.src[start : start+n], File: d.file, Offset: start}
		at := &d.at
		if mode == 3 {
			// A cursor of its own, from the start of the line before the rule.
			c := cursor{off: bytes.LastIndexByte(d.src[:start], '\n') + 1, row: d.row - bytes.Count(d.src[start:d.base], []byte("\n"))}
			at = &c
		} else if start < d.at.off {
			d.at = d.start()
		}
		at.advance(d.src, start)
		l.Row, l.Col = at.row, at.runes+1
		if len(at.tabs) > 0 {
			l.Tabs = slices.Clone(at.tabs)
		}
		if start < len(d.src) && d.src[start] == '\t' {
			l.Tabs = append(l.Tabs, l.Col)
		}
		return l
	}
	l := &ast.Location{Text: d.take(d.uvarint()), Offset: d.uvarint(), File: d.str()}
	l.Row, l.Col = d.uvarint(), d.uvarint()
	if n := d.uvarint(); n > 0 && d.err == nil {
		l.Tabs = make([]int, n)
		for i := range l.Tabs {
			l.Tabs[i] = d.uvarint()
		}
	}
	if d.bare {
		return nil
	}
	return l
}

type cursor struct {
	off, row, runes int
	tabs            []int
}

// start is the cursor at the rule's offset.
func (d *decoder) start() cursor {
	c := cursor{off: bytes.LastIndexByte(d.src[:d.base], '\n') + 1, row: d.row}
	c.advance(d.src, d.base)
	return c
}

func (c *cursor) advance(src []byte, to int) {
	for c.off < to {
		r, w := utf8.DecodeRune(src[c.off:])
		if r == '\n' {
			c.row, c.runes, c.tabs = c.row+1, 0, c.tabs[:0]
		} else {
			c.runes++
			if r == '\t' {
				c.tabs = append(c.tabs, c.runes)
			}
		}
		c.off += w
	}
}

func (d *decoder) rule(module *ast.Module) *ast.Rule {
	flags := d.byte()
	r := &ast.Rule{Default: flags&ruleDefault != 0, Module: module, Location: d.loc()}
	r.Annotations = d.annotations(r)
	r.Head = d.head()
	r.Body = d.body()
	if flags&ruleElse != 0 && d.err == nil {
		r.Else = d.rule(module)
	}
	return r
}

func (d *decoder) annotations(rule *ast.Rule) []*ast.Annotations {
	n := d.uvarint()
	if n == 0 || d.err != nil {
		return nil
	}
	var out []*ast.Annotations
	if !d.bare {
		out = make([]*ast.Annotations, n)
	}
	for i := range n {
		var a ast.Annotations
		a.Scope = d.str()
		var labels map[string]any
		switch d.byte() {
		case labelsStrings:
			n := d.uvarint()
			if d.err != nil {
				break
			}
			if d.bare {
				for range 2 * n {
					d.str()
				}
				break
			}
			labels = make(map[string]any, n)
			for range n {
				k := d.str()
				labels[k] = d.str()
			}
		case labelsTerm:
			if t := d.term(); t != nil && d.err == nil {
				x, err := ast.JSON(t.Value)
				if m, ok := x.(map[string]any); err == nil && ok {
					labels = m
				} else {
					d.fail(errors.New("compiled rules: malformed annotation labels"))
				}
			}
		case labelsNone:
		default:
			d.fail(errors.New("compiled rules: malformed annotation labels"))
		}
		if bs := d.take(d.uvarint()); len(bs) > 0 && d.err == nil && !d.bare {
			scope := a.Scope
			if err := json.Unmarshal(bs, &a); err != nil {
				d.fail(err)
			}
			a.Scope = scope
		}
		a.Location = d.loc()
		if d.bare {
			continue
		}
		// Copy attaches the annotations to the rule, see ast.AnnotationSet.Chain.
		out[i] = a.Copy(rule)
		out[i].Labels = labels
	}
	return out
}

func (d *decoder) head() *ast.Head {
	flags := d.byte()
	h := &ast.Head{Assign: flags&headAssign != 0, Location: d.loc()}
	h.Name = ast.Var(d.str())
	h.Reference = d.ref()
	if n := d.uvarint(); n > 0 {
		h.Args = make(ast.Args, n)
		for i := range h.Args {
			h.Args[i] = d.term()
		}
	}
	if flags&headKey != 0 {
		h.Key = d.term()
	}
	if flags&headValue != 0 {
		h.Value = d.term()
	}
	return h
}

func (d *decoder) body() ast.Body {
	n := d.uvarint()
	if d.err != nil {
		return nil
	}
	b := make(ast.Body, n)
	for i := range b {
		b[i] = d.expr()
	}
	return b
}

func (d *decoder) expr() *ast.Expr {
	flags := d.byte()
	x := &ast.Expr{Index: d.uvarint(), Negated: flags&flagNegated != 0, Generated: flags&flagGenerated != 0}
	x.Location = d.loc()
	switch kind := d.byte(); kind {
	case exprTerm:
		x.Terms = d.term()
	case exprCall:
		x.Terms = d.terms()
	case exprSomeDecl:
		sd := &ast.SomeDecl{Location: d.loc()}
		sd.Symbols = d.terms()
		x.Terms = sd
	case exprEvery:
		q := &ast.Every{}
		hasKey := d.byte() != 0
		q.Location = d.loc()
		if hasKey {
			q.Key = d.term()
		}
		q.Value, q.Domain, q.Body = d.term(), d.term(), d.body()
		x.Terms = q
	case exprNot:
		n := &ast.Not{ExplicitBody: d.byte() != 0}
		n.Location = d.loc()
		n.Body = d.body()
		x.Terms = n
	case exprAnd:
		a := &ast.LogicalAnd{ExplicitLhs: d.byte() != 0, ExplicitRhs: d.byte() != 0}
		a.Location = d.loc()
		a.Lhs, a.Rhs = d.body(), d.body()
		x.Terms = a
	case exprOr:
		o := &ast.LogicalOr{ExplicitLhs: d.byte() != 0, ExplicitRhs: d.byte() != 0}
		o.Location = d.loc()
		o.Lhs, o.Rhs = d.body(), d.body()
		x.Terms = o
	default:
		d.fail(fmt.Errorf("compiled rules: unknown expression kind %d", kind))
	}
	if flags&flagWith != 0 {
		n := d.uvarint()
		if d.err == nil {
			x.With = make([]*ast.With, 0, n)
		}
		for range n {
			w := &ast.With{Location: d.loc()}
			w.Target, w.Value = d.term(), d.term()
			x.With = append(x.With, w)
		}
	}
	return x
}

func (d *decoder) terms() []*ast.Term {
	n := d.uvarint()
	if d.err != nil {
		return nil
	}
	ts := make([]*ast.Term, n)
	for i := range ts {
		ts[i] = d.term()
	}
	return ts
}

func (d *decoder) ref() ast.Ref {
	if n := d.uvarint(); n > 0 && d.err == nil {
		r := make(ast.Ref, n)
		for i := range r {
			r[i] = d.term()
		}
		return r
	}
	return nil
}

func (d *decoder) term() *ast.Term {
	loc := d.loc()
	v, interned := d.value()
	if interned != nil && loc == nil {
		return interned
	}
	return &ast.Term{Value: v, Location: loc}
}

// value decodes a term's value, and gives the interned term for it, if any.
func (d *decoder) value() (ast.Value, *ast.Term) {
	switch tag := d.byte(); tag {
	case tagNull:
		return ast.Null{}, ast.NullTerm()
	case tagTrue:
		return ast.Boolean(true), ast.InternedTerm(true)
	case tagFalse:
		return ast.Boolean(false), ast.InternedTerm(false)
	case tagNumber:
		return ast.Number(d.str()), nil
	case tagString:
		return ast.String(d.str()), nil
	case tagVar:
		return ast.InternedVarValue(d.str()), nil
	case tagRef:
		return ast.Ref(d.terms()), nil
	case tagArray:
		return ast.NewArray(d.terms()...), nil
	case tagSet:
		return ast.NewSet(d.terms()...), nil
	case tagObject:
		n := d.uvarint()
		pairs := make([][2]*ast.Term, 0, n)
		for range n {
			if d.err != nil {
				break
			}
			pairs = append(pairs, [2]*ast.Term{d.term(), d.term()})
		}
		return ast.NewObject(pairs...), nil
	case tagArrayComprehension:
		return &ast.ArrayComprehension{Term: d.term(), Body: d.body()}, nil
	case tagSetComprehension:
		return &ast.SetComprehension{Term: d.term(), Body: d.body()}, nil
	case tagObjectComprehension:
		return &ast.ObjectComprehension{Key: d.term(), Value: d.term(), Body: d.body()}, nil
	case tagCall:
		return ast.Call(d.terms()), nil
	default:
		d.fail(fmt.Errorf("compiled rules: unknown term tag %d", tag))
		return ast.Null{}, ast.NullTerm()
	}
}
