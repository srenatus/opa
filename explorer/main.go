package main

import (
	"syscall/js"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/format"
)

func main() {
	done := make(chan struct{})
	js.Global().Set("opa", make(map[string]interface{}))
	module := js.Global().Get("opa")
	module.Set("compile_stages",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if args == nil {
				panic("initialize: not enough args")
			}

			results := ast.CompilerStages(args[0].String())
			res := make([]interface{}, len(results))
			for i, s := range results {
				m := map[string]interface{}{
					"stage": s.Stage,
				}
				if s.Error != "" {
					m["error"] = s.Error
				} else {
					m["result"] = formatMod(s.Result)
				}
				res[i] = m
			}
			return res
		}),
	)

	<-done
}

func formatMod(m *ast.Module) string {
	out, err := format.Ast(m)
	if err != nil {
		panic(err)
	}
	return string(out)
}
