# Example Source Provider Plugin

This package demonstrates how to create a plugin that provides external rule sources to OPA. Plugin authors can use this as a template for their own implementations.

## Overview

The example plugin shows the complete integration path:
1. Implementing the OPA plugin interface ([`Factory`](plugin.go:33) and [`Plugin`](plugin.go:47))
2. Registering external rule sources with the compiler
3. Using existing `exp/sp` sources (like [`StaticSource`](../source_provider.go))

## Key Integration Points

### 1. Plugin Lifecycle

The plugin implements the standard OPA plugin interface:

```go
type Plugin interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context)
    Reconfigure(ctx context.Context, config any)
}
```

### 2. Compiler Integration

External sources are registered with the compiler so they're available during policy evaluation:

```go
compiler.WithExternalSource(pkgRef, source)
```

This is done in [`Start()`](plugin.go:65) using both immediate registration and a compiler trigger for updates.

### 3. Using Existing Sources

The example uses [`sp.NewStaticSource()`](plugin.go:90) from `exp/sp`, but plugin authors can implement any [`ast.ExternalRuleSource`](../../v1/ast/external_source.go:11):

```go
type ExternalRuleSource interface {
    Refs() []Ref
    Init(context.Context) (ExternalRuleIndex, error)
}
```

## Usage

To use this pattern in your own plugin:

1. Copy [`plugin.go`](plugin.go) as a template
2. Modify the configuration structure for your needs
3. Replace the static source with your own implementation
4. Register your plugin with OPA

## Configuration Example

```yaml
plugins:
  example_source_provider:
    package_ref: "data.external.authz"
    rules:
      - "package external.authz\nallow := true if { input.admin }"
```

## Testing

Run the tests to see the complete integration:

```bash
go test ./exp/sp/example/...
```

The tests demonstrate:
- Plugin lifecycle (start/stop)
- Compiler integration
- External source registration
- Rule retrieval

## For Plugin Authors

When creating your own plugin:

- **Keep it simple**: This example is intentionally minimal
- **Focus on your source logic**: The plugin plumbing is straightforward
- **Use existing sources**: The `exp/sp` package provides helpful utilities
- **Test end-to-end**: Verify rules are accessible through the compiler

The key insight is that `exp/sp` sources work directly with the OPA plugin system - you just need to wrap them in the plugin lifecycle interface.
