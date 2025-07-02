package tofu

import (
	"context"
	"fmt"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

func WalkValidate(ctx context.Context, config *configs.Config, tofuCtx *Context) tfdiags.Diagnostics {
	scope := NewRootScope(walkValidate, tofuCtx, states.NewState(), states.NewState(), states.NewState(), plans.NewChanges(), config)
	inputs := VariableInputs{}

	// Mirrors tofu/context_validate.go
	for name, variable := range config.Module.Variables {
		ty := variable.Type
		if ty == cty.NilType {
			// Can't predict the type at all, so we'll just mark it as
			// cty.DynamicVal (unknown value of cty.DynamicPseudoType).
			ty = cty.DynamicPseudoType
		}
		inputs[addrs.InputVariable{Name: name}] = VariableInput{
			expr: &hclsyntax.LiteralValueExpr{Val: cty.UnknownVal(ty)},
		}
	}

	root := NewModule(ctx, addrs.RootModuleInstance, config, inputs, scope)

	p := NewManager(tofuCtx.Semaphore())
	root.Collect(p)
	edges, diags := p.Wait()
	//spew.Dump(edges)
	fmt.Printf("Detected %v edges", len(edges))

	return diags
}
