package ast

type CompileResult struct {
	Stage  string
	Result *Module
}

func CompilerStages(rego string) []CompileResult {
	c := NewCompiler().
		WithEnablePrintStatements(true)

	result := make([]CompileResult, len(c.stages))
	for i, s := range c.stages {
		idx := i
		stage := s
		c = c.WithStageAfter(stage.name,
			CompilerStageDefinition{
				Name:       stage.name + "Record",
				MetricName: stage.metricName + "_record",
				Stage: func(c0 *Compiler) *Error {
					result[idx] = CompileResult{
						Stage:  stage.name,
						Result: getOne(c0.Modules),
					}
					return nil
				},
			})
	}
	c.Compile(map[string]*Module{
		"a.rego": MustParseModule(rego),
	})
	if len(c.Errors) > 0 {
		panic(c.Errors)
	}
	return result
}

func getOne(mods map[string]*Module) *Module {
	for _, m := range mods {
		return m.Copy()
	}
	panic("unreachable")
}
