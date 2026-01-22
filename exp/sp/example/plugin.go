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
	PackageRefs []string `json:"package_refs"`
	Rules       []string `json:"rules"`
}

type Plugin struct {
	manager *plugins.Manager
	config  Config
	source  ast.ExternalRuleSource
	pkgRefs []ast.Ref
	logger  logging.Logger
}

type Factory struct{}

func (Factory) Validate(manager *plugins.Manager, config []byte) (any, error) {
	var parsedConfig Config
	if err := json.Unmarshal(config, &parsedConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if len(parsedConfig.PackageRefs) == 0 {
		return nil, fmt.Errorf("package_refs is required")
	}

	for i, pkgRef := range parsedConfig.PackageRefs {
		if _, err := ast.ParseRef(pkgRef); err != nil {
			return nil, fmt.Errorf("invalid package_refs[%d]: %w", i, err)
		}
	}

	return parsedConfig, nil
}

func (Factory) New(manager *plugins.Manager, config any) plugins.Plugin {
	parsedConfig := config.(Config)
	logger := manager.Logger().WithFields(map[string]any{"plugin": PluginName})

	pkgRefs := make([]ast.Ref, 0, len(parsedConfig.PackageRefs))
	for _, pkgRefStr := range parsedConfig.PackageRefs {
		pkgRefs = append(pkgRefs, ast.MustParseRef(pkgRefStr))
	}

	rulesByPkg := make(map[string][]*ast.Rule)
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

		var modulePkgRef ast.Ref
		if compiledModule.Package.Path[0].Equal(ast.DefaultRootDocument) {
			modulePkgRef = compiledModule.Package.Path
		} else {
			modulePkgRef = ast.Ref([]*ast.Term{ast.DefaultRootDocument}).Concat(compiledModule.Package.Path)
		}
		pkgRefStr := modulePkgRef.String()

		for _, rule := range compiledModule.Rules {
			rulesByPkg[pkgRefStr] = append(rulesByPkg[pkgRefStr], rule)
		}
	}

	source := sp.NewStaticSourceFromMap(pkgRefs, rulesByPkg)

	for _, pkgRef := range pkgRefs {
		logger.Debug("Registering external source %v with manager", pkgRef.String())
		manager.RegisterExternalSource(pkgRef, source)
	}

	return &Plugin{
		manager: manager,
		config:  parsedConfig,
		source:  source,
		pkgRefs: pkgRefs,
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
