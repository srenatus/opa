package topdown_test

import (
	"testing"

	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/util"
)

func TestRepeatedRefsEvaluateIndependently(t *testing.T) {
	t.Parallel()

	module := `package test

p if {
	input.x > 0
	input.x < 10
	input.x != 5
}

with_override if {
	input.x == 1
	input.x == 2 with input.x as 2
}

with_comprehension := [y | input.x == 1; y := [z | z := input.x] with input.x as 2]

in_not if {
	input.x == 1
	not input.x == 2
}

not_then_use if {
	not input.y == 2
	input.y == 1
}

in_not_body if {
	not count([1 | input.x == 2]) > 0
	input.x == 1
}

every_body if {
	input.x == 1
	every i in [1, 2] { input.x < i + 1 }
}
`
	for _, tc := range []struct {
		rule  string
		input string
		exp   string
	}{
		{"p", `{"x": 3}`, `true`},
		{"p", `{"x": 5}`, `undefined`},
		{"with_override", `{"x": 1}`, `true`},
		{"with_comprehension", `{"x": 1}`, `[[2]]`},
		{"in_not", `{"x": 1}`, `true`},
		{"not_then_use", `{"y": 1}`, `true`},
		{"in_not_body", `{"x": 1}`, `true`},
		{"every_body", `{"x": 1}`, `true`},
	} {
		t.Run(tc.rule+tc.input, func(t *testing.T) {
			rs, err := rego.New(
				rego.Module("test.rego", module),
				rego.Query("data.test."+tc.rule),
				rego.Input(util.MustUnmarshalJSON([]byte(tc.input))),
			).Eval(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if tc.exp == "undefined" {
				if len(rs) != 0 {
					t.Fatalf("expected undefined, got %v", rs)
				}
				return
			}
			if len(rs) != 1 {
				t.Fatalf("expected one result, got %v", rs)
			}
			exp := util.MustUnmarshalJSON([]byte(tc.exp))
			if util.Compare(exp, rs[0].Expressions[0].Value) != 0 {
				t.Fatalf("expected %v, got %v", exp, rs[0].Expressions[0].Value)
			}
		})
	}
}
