package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/open-policy-agent/opa/ast"
)

func main() {
	js.Global().Set("parseModule", js.FuncOf(jsonParseModule))
	select {} // Code must not finish // TODO(sr): really?)
}

func jsonParseModule(this js.Value, args []js.Value) interface{} {
	jsDoc := js.Global().Get("document")
	if !jsDoc.Truthy() {
		return "Unable to get document object"
	}
	codeText := jsDoc.Call("getElementById", "rego")
	if !codeText.Truthy() {
		return "Unable to get rego text area"
	}
	resultText := jsDoc.Call("getElementById", "result")
	if !resultText.Truthy() {
		return "Unable to get result text area"
	}
	errText := jsDoc.Call("getElementById", "error")
	if !errText.Truthy() {
		return "Unable to get error text area"
	}

	code := codeText.Get("value").String()
	x, err := ast.ParseModule("policy.rego", code)
	if err != nil {
		errText.Set("value", err.Error())
		return nil
	}

	bs, err := json.MarshalIndent(x, "", "  ")
	if err != nil {
		errText.Set("value", err.Error())
		return nil
	}
	resultText.Set("value", string(bs))
	return nil
}
