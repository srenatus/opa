// Copyright 2026 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package exampleplugin demonstrates how to create a plugin that provides
// external rule sources. Plugin authors can use this as a template.
package exampleplugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-policy-agent/opa/exp/sp"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/logging"
	"github.com/open-policy-agent/opa/v1/plugins"
)

const PluginName = "example_source_provider"

type Config struct {
	PackageRef string   `json:"package_ref"`
	Rules      []string `json:"rules"`
}

type Plugin struct {
	manager *plugins.Manager
	config  Config
	source  ast.ExternalRuleSource
	pkgRef  ast.Ref
	logger  logging.Logger
}

type Factory struct{}

func (Factory) Validate(manager *plugins.Manager, config []byte) (any, error) {
	var parsedConfig Config
	if err := json.Unmarshal(config, &parsedConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if parsedConfig.PackageRef == "" {
		return nil, fmt.Errorf("package_ref is required")
	}

	if _, err := ast.ParseRef(parsedConfig.PackageRef); err != nil {
		return nil, fmt.Errorf("invalid package_ref: %w", err)
	}

	return parsedConfig, nil
}

func (Factory) New(manager *plugins.Manager, config any) plugins.Plugin {
	parsedConfig := config.(Config)
	logger := manager.Logger().WithFields(map[string]any{"plugin": PluginName})

	pkgRef := ast.MustParseRef(parsedConfig.PackageRef)

	var rules []*ast.Rule
	for i, ruleStr := range parsedConfig.Rules {
		moduleName := fmt.Sprintf("rule%d", i)
		module, err := ast.ParseModuleWithOpts(
			moduleName,
			ruleStr,
			manager.ParserOptions(),
		)
		if err != nil {
			panic(fmt.Sprintf("failed to parse rule %d: %v", i, err))
		}

		compiler := ast.NewCompiler()
		compiler.Compile(map[string]*ast.Module{moduleName: module})
		if compiler.Failed() {
			panic(fmt.Sprintf("failed to compile rule %d: %v", i, compiler.Errors))
		}

		compiledModule := compiler.Modules[moduleName]
		for _, rule := range compiledModule.Rules {
			rules = append(rules, rule)
		}
	}

	source := sp.NewStaticSource([]ast.Ref{pkgRef}, rules)

	// Register external source with manager - this ensures it will be
	// applied to all compilers BEFORE they are compiled
	logger.Debug("Registering external source %v with manager", pkgRef.String())
	manager.RegisterExternalSource(pkgRef, source)

	return &Plugin{
		manager: manager,
		config:  parsedConfig,
		source:  source,
		pkgRef:  pkgRef,
		logger:  logger,
	}
}

func (p *Plugin) Start(ctx context.Context) error {
	p.logger.Info("Starting example source provider plugin.")
	p.manager.UpdatePluginStatus(PluginName, &plugins.Status{State: plugins.StateOK})
	return nil
}

func (p *Plugin) Stop(context.Context) {
	p.logger.Info("Stopping example source provider plugin.")
}

func (p *Plugin) Reconfigure(context.Context, any) {
	// TODO(sr): We could have gotten a new config, reconfigure
	p.logger.Info("Reconfiguring example source provider plugin.")
}
