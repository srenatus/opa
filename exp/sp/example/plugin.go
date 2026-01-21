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
	"github.com/open-policy-agent/opa/v1/storage"
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
	return &Plugin{
		manager: manager,
		config:  config.(Config),
		logger:  manager.Logger().WithFields(map[string]any{"plugin": PluginName}),
	}
}

func (p *Plugin) Start(ctx context.Context) error {
	p.logger.Info("Starting example source provider plugin.")

	pkgRef, err := ast.ParseRef(p.config.PackageRef)
	if err != nil {
		return fmt.Errorf("invalid package_ref: %w", err)
	}

	var rules []*ast.Rule
	for i, ruleStr := range p.config.Rules {
		module, err := ast.ParseModuleWithOpts(
			fmt.Sprintf("rule%d", i),
			ruleStr,
			p.manager.ParserOptions(),
		)
		if err != nil {
			return fmt.Errorf("failed to parse rule %d: %w", i, err)
		}
		for _, rule := range module.Rules {
			rules = append(rules, rule)
		}
	}

	p.source = sp.NewStaticSource([]ast.Ref{pkgRef}, rules)

	p.logger.Info("Registering external source for package: %s", pkgRef.String())

	p.manager.RegisterCompilerTrigger(func(storage.Transaction) {
		compiler := p.manager.GetCompiler()
		p.logger.Debug("Compiler trigger fired, compiler available: %v", compiler != nil)
		if compiler != nil {
			p.logger.Debug("Registering external source %v with compiler<%p>", pkgRef.String(), compiler)
			compiler.WithExternalSource(pkgRef, p.source)
		}
	})

	if compiler := p.manager.GetCompiler(); compiler != nil {
		p.logger.Debug("Initial registration of external source %v with compiler<%p>", pkgRef.String(), compiler)
		compiler.WithExternalSource(pkgRef, p.source)
	}

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
