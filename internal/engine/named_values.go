package engine

import "github.com/opentofu/opentofu/internal/configs"

type Variable struct {
	Module *Module
	Config *configs.Variable
}

type Output struct {
	Module *Module
	Config *configs.Output
}

type Local struct {
	Module *Module
	Config *configs.Local
}
