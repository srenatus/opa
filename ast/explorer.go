package ast

type CompileResult struct {
	Stage  string
	Result *Module
	Error  string
}

func CompilerStages(rego string) []CompileResult {
	c := NewCompiler().
		WithEnablePrintStatements(true)

	result := make([]CompileResult, 0, len(c.stages)+1)
	result = append(result, CompileResult{
		Stage: "ParseModule",
	})
	mod, err := ParseModule("a.rego", rego)
	if err != nil {
		result[0].Error = err.Error()
		return result
	}
	result[0].Result = mod

	for i := range c.stages {
		stage := c.stages[i]
		c = c.WithStageAfter(stage.name,
			CompilerStageDefinition{
				Name:       stage.name + "Record",
				MetricName: stage.metricName + "_record",
				Stage: func(c0 *Compiler) *Error {
					result = append(result, CompileResult{
						Stage:  stage.name,
						Result: getOne(c0.Modules),
					})
					return nil
				},
			})
	}
	c.Compile(map[string]*Module{
		"a.rego": mod,
	})
	if len(c.Errors) > 0 {
		result[len(result)-1].Error = c.Errors.Error()
	}
	return result
}

func getOne(mods map[string]*Module) *Module {
	for _, m := range mods {
		return m.Copy()
	}
	panic("unreachable")
}
