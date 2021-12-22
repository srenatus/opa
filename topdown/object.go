// Copyright 2020 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package topdown

import (
	"fmt"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/topdown/builtins"
	"github.com/open-policy-agent/opa/types"
)

func builtinObjectUnion(_ BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	objA, err := builtins.ObjectOperand(operands[0].Value, 1)
	if err != nil {
		return err
	}

	objB, err := builtins.ObjectOperand(operands[1].Value, 2)
	if err != nil {
		return err
	}

	r := mergeWithOverwrite(objA, objB)

	return iter(ast.NewTerm(r))
}

func builtinObjectRemove(_ BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	// Expect an object and an array/set/object of keys
	obj, err := builtins.ObjectOperand(operands[0].Value, 1)
	if err != nil {
		return err
	}

	// Build a set of keys to remove
	keysToRemove, err := getObjectKeysParam(operands[1].Value)
	if err != nil {
		return err
	}
	r := ast.NewObject()
	obj.Foreach(func(key *ast.Term, value *ast.Term) {
		if !keysToRemove.Contains(key) {
			r.Insert(key, value)
		}
	})

	return iter(ast.NewTerm(r))
}

func builtinObjectFilter(_ BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	// Expect an object and an array/set/object of keys
	obj, err := builtins.ObjectOperand(operands[0].Value, 1)
	if err != nil {
		return err
	}

	// Build a new object from the supplied filter keys
	keys, err := getObjectKeysParam(operands[1].Value)
	if err != nil {
		return err
	}

	filterObj := ast.NewObject()
	keys.Foreach(func(key *ast.Term) {
		filterObj.Insert(key, ast.NullTerm())
	})

	// Actually do the filtering
	r, err := obj.Filter(filterObj)
	if err != nil {
		return err
	}

	return iter(ast.NewTerm(r))
}

func builtinObjectGet(_ BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	object, err := builtins.ObjectOperand(operands[0].Value, 1)
	if err != nil {
		return err
	}

	if ret := object.Get(operands[1]); ret != nil {
		return iter(ret)
	}

	return iter(operands[2])
}

func builtinObjectDig(_ BuiltinContext, operands []*ast.Term, iter func(*ast.Term) error) error {
	// top level object input must type check as an Object
	object, err := builtins.ObjectOperand(operands[0].Value, 1)
	if err != nil {
		return err
	}

	// paths must type check as Array
	path, err := builtins.ArrayOperand(operands[1].Value, 2)
	if err != nil {
		return err
	}

	// if the path is empty, then we skip digging and return the default
	if path.Len() == 0 {
		return iter(operands[2])
	}

	var dugTerm *ast.Term

	// index is used to track if the term being processed is the last in the path
	var index int
	err = path.Iter(func(term *ast.Term) error {
		index++

		// dugTerm will either be the return value, or the object to dig for the next iteration
		dugTerm = object.Get(term)
		if dugTerm == nil {
			return fmt.Errorf("dugTerm not found")
		}

		// if processing the final element, then we exit with whatever the current dugTerm is
		if index == path.Len() {
			return nil
		}

		// attempt to update the object to dig on the next iteration to the current dugTerm,
		// if this fails, then the default value will be returned
		var ok bool
		object, ok = dugTerm.Value.(ast.Object)
		if !ok {
			return fmt.Errorf("dugTerm was not object")
		}

		return nil
	})

	// err is set while working down the path when either:
	// A) an intermediate path key did not have an object value
	// B) a path key was not found at the required depth during the search
	if err != nil {
		return iter(operands[2])
	}

	return iter(dugTerm)
}

// getObjectKeysParam returns a set of key values
// from a supplied ast array, object, set value
func getObjectKeysParam(arrayOrSet ast.Value) (ast.Set, error) {
	keys := ast.NewSet()

	switch v := arrayOrSet.(type) {
	case *ast.Array:
		_ = v.Iter(func(f *ast.Term) error {
			keys.Add(f)
			return nil
		})
	case ast.Set:
		_ = v.Iter(func(f *ast.Term) error {
			keys.Add(f)
			return nil
		})
	case ast.Object:
		_ = v.Iter(func(k *ast.Term, _ *ast.Term) error {
			keys.Add(k)
			return nil
		})
	default:
		return nil, builtins.NewOperandTypeErr(2, arrayOrSet, ast.TypeName(types.Object{}), ast.TypeName(types.S), ast.TypeName(types.Array{}))
	}

	return keys, nil
}

func mergeWithOverwrite(objA, objB ast.Object) ast.Object {
	merged, _ := objA.MergeWith(objB, func(v1, v2 *ast.Term) (*ast.Term, bool) {
		originalValueObj, ok2 := v1.Value.(ast.Object)
		updateValueObj, ok1 := v2.Value.(ast.Object)
		if !ok1 || !ok2 {
			// If we can't merge, stick with the right-hand value
			return v2, false
		}

		// Recursively update the existing value
		merged := mergeWithOverwrite(originalValueObj, updateValueObj)
		return ast.NewTerm(merged), false
	})
	return merged
}

func init() {
	RegisterBuiltinFunc(ast.ObjectUnion.Name, builtinObjectUnion)
	RegisterBuiltinFunc(ast.ObjectRemove.Name, builtinObjectRemove)
	RegisterBuiltinFunc(ast.ObjectFilter.Name, builtinObjectFilter)
	RegisterBuiltinFunc(ast.ObjectGet.Name, builtinObjectGet)
	RegisterBuiltinFunc(ast.ObjectDig.Name, builtinObjectDig)
}
