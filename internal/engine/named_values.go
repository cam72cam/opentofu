package engine

import "github.com/opentofu/opentofu/internal/configs"

type Variable struct {
	Module *Module
	Config *configs.Variable
}

type Output struct{}

type VariableInstance struct {
	Variable *Variable
}
type OutputInstance struct {
	Output *Output
}
