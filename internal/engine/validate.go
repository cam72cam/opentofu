package engine

import (
	"context"
	"fmt"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/plans"
	"github.com/opentofu/opentofu/internal/plugins"
	"github.com/opentofu/opentofu/internal/states"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type Validate func() tfdiags.Diagnostics

func ValidatePromise[T any](p *Promise[T]) Validate {
	return func() tfdiags.Diagnostics {
		_, diags := p.Value(nil)
		return diags
	}
}

type Validates []Validate

func (s Validates) Collect() tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics
	for _, v := range s {
		diags = diags.Append(v())
	}
	return diags
}

func WalkValidate(ctx context.Context, config *configs.Config, plugins plugins.Manager, hooks []tofu.Hook, workspace string) tfdiags.Diagnostics {
	scope := NewRootScope(walkValidate, plugins, hooks, workspace, states.NewState().SyncWrapper(), states.NewState().SyncWrapper(), states.NewState().SyncWrapper(), plans.NewChanges().SyncWrapper(), config)
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

	p := NewConcurrencyPool(1)
	root.Collect(p)
	edges, diags := p.Wait()
	//spew.Dump(edges)
	fmt.Printf("Detected %v edges", len(edges))

	return diags
}
