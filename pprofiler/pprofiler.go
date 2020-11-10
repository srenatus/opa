// Copyright 2020 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package pprofiler computes and reports on the time spent on expressions,
// in pprof format.
package pprofiler

import (
	"time"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown"
)

// Pprofiler computes and reports on the time spent on expressions.
type Pprofiler struct {
	hits        map[string]map[int]ExprStats
	activeTimer time.Time
	prevExpr    exprInfo
}

// exprInfo stores information about an expression.
type exprInfo struct {
	location *ast.Location
	op       topdown.Op
}

// New returns a new Pprofiler object.
func New() *Pprofiler {
	return &Pprofiler{
		hits: map[string]map[int]ExprStats{},
	}
}

// Enabled returns true if profiler is enabled.
func (p *Pprofiler) Enabled() bool {
	return true
}

// Config returns the standard Tracer configuration for the pprofiler
func (p *Pprofiler) Config() topdown.TraceConfig {
	return topdown.TraceConfig{
		PlugLocalVars: false, // Event variable metadata is not required for the Pprofiler
	}
}

// Trace updates the profiler state.
// Deprecated: Use TraceEvent instead.
func (p *Pprofiler) Trace(event *topdown.Event) {
	p.TraceEvent(*event)
}

// TraceEvent records the event for profiling.
func (p *Pprofiler) TraceEvent(event topdown.Event) {
	switch event.Op {
	case topdown.EvalOp:
		if expr, ok := event.Node.(*ast.Expr); ok && expr != nil {
			p.processExpr(expr, event.Op)
		}
	case topdown.RedoOp:
		if expr, ok := event.Node.(*ast.Expr); ok && expr != nil {
			p.processExpr(expr, event.Op)
		}
	}
}

func (p *Pprofiler) processExpr(expr *ast.Expr, eventType topdown.Op) {
	if expr.Location == nil {
		// add fake location to group expressions without a location
		expr.Location = ast.NewLocation([]byte("???"), "", 0, 0)
	}

	// set the active timer on the first expression
	if p.activeTimer.IsZero() {
		p.activeTimer = time.Now()
		p.prevExpr = exprInfo{
			op:       eventType,
			location: expr.Location,
		}
		return
	}

	// record the profiler results for the previous expression
	file := p.prevExpr.location.File
	hits, ok := p.hits[file]
	if !ok {
		hits = map[int]ExprStats{}
		hits[p.prevExpr.location.Row] = getPprofilerStats(p.prevExpr, p.activeTimer)
		p.hits[file] = hits
	} else {
		pos := p.prevExpr.location.Row
		pStats, ok := hits[pos]
		if !ok {
			hits[pos] = getPprofilerStats(p.prevExpr, p.activeTimer)
		} else {
			pStats.ExprTimeNs += time.Since(p.activeTimer).Nanoseconds()

			switch p.prevExpr.op {
			case topdown.EvalOp:
				pStats.NumEval++
			case topdown.RedoOp:
				pStats.NumRedo++
			}
			hits[pos] = pStats
		}
	}

	// reset active timer and expression
	p.activeTimer = time.Now()
	p.prevExpr = exprInfo{
		op:       eventType,
		location: expr.Location,
	}
}

// TODO(sr): ensure the last thing is recorded, too
func (p *Pprofiler) processLastExpr() {
	expr := ast.Expr{
		Location: p.prevExpr.location,
	}
	p.processExpr(&expr, p.prevExpr.op)
}

func getPprofilerStats(expr exprInfo, timer time.Time) ExprStats {
	profilerStats := ExprStats{}
	profilerStats.ExprTimeNs = time.Since(timer).Nanoseconds()
	profilerStats.Location = expr.location

	switch expr.op {
	case topdown.EvalOp:
		profilerStats.NumEval = 1
	case topdown.RedoOp:
		profilerStats.NumRedo = 1
	}
	return profilerStats
}
