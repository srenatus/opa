// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package merge

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/open-policy-agent/opa/util"
)

func BenchmarkInterfaceMaps(b *testing.B) {

	for _, size := range []int{100, 1000, 10000} {

		b.Run(fmt.Sprintf("store size %v", size), func(b *testing.B) {

			bVals := map[string]interface{}{
				"a": map[string]interface{}{"b": "c", "d": "e"},
			}

			for i := 0; i < b.N; i++ {
				a := aVal(size)
				_, ok := InterfaceMaps(a, bVals)
				if !ok {
					b.Fatal("merging interfaces failed")
				}
			}
		})
	}
}

func aVal(size int) map[string]interface{} {
	r := map[string]interface{}{}
	for i := 0; i < size; i++ {
		r[fmt.Sprintf("%d", i)] = map[string]interface{}{
			"a": map[string]interface{}{"b": "c", "d": "e"},
		}
	}
	return r
}

func TestMergeDocs(t *testing.T) {

	tests := []struct {
		a  string
		b  string
		c  string
		ok bool
	}{
		{`{"x": 1, "y": 2}`, `{"z": 3}`, `{"x": 1, "y": 2, "z": 3}`, true},
		{`{"x": {"y": 2}}`, `{"z": 3, "x": {"q": 4}}`, `{"x": {"y": 2, "q": 4}, "z": 3}`, true},
		{`{"x": 1}`, `{"x": 1}`, "", false},
		{`{"x": {"y": [{"z": 2}]}}`, `{"x": {"y": [{"z": 3}]}}`, "", false},
	}

	for _, tc := range tests {
		a := map[string]interface{}{}
		if err := util.UnmarshalJSON([]byte(tc.a), &a); err != nil {
			panic(err)
		}
		aInitial := map[string]interface{}{}
		if err := util.UnmarshalJSON([]byte(tc.a), &aInitial); err != nil {
			panic(err)
		}

		b := map[string]interface{}{}
		if err := util.UnmarshalJSON([]byte(tc.b), &b); err != nil {
			panic(err)
		}

		if len(tc.c) == 0 {

			c, ok := InterfaceMaps(a, b)
			if ok {
				t.Errorf("Expected merge(%v,%v) == false but got: %v", a, b, c)
			}

			if !reflect.DeepEqual(a, aInitial) {
				t.Errorf("Expected conflicting merge to not mutate a (%v) but got a: %v", aInitial, a)
			}

		} else {

			expected := map[string]interface{}{}
			if err := util.UnmarshalJSON([]byte(tc.c), &expected); err != nil {
				panic(err)
			}

			c, ok := InterfaceMaps(a, b)
			if !ok || !reflect.DeepEqual(c, expected) {
				t.Errorf("Expected merge(%v, %v) == %v but got: %v (ok: %v)", a, b, expected, c, ok)
			}

			if reflect.DeepEqual(a, aInitial) || !reflect.DeepEqual(a, c) {
				t.Errorf("Expected merge to mutate a (%v) but got %v", aInitial, a)
			}

		}
	}
}
