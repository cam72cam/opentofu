package engine

import (
	"context"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/lang/marks"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type VariableInput struct {
	value *tofu.InputValue
	expr  hcl.Expression
}
type VariableInputs map[addrs.InputVariable]VariableInput

func NewRootVariableInputs(values tofu.InputValues) VariableInputs {
	inputs := VariableInputs{}
	for name, value := range values {
		inputs[addrs.InputVariable{Name: name}] = VariableInput{value: value}
	}
	return inputs
}

func NewVariable(ctx context.Context, addr addrs.AbsInputVariableInstance, config *configs.Variable, input VariableInput, scope *Scope, parentScope *Scope) *Promise[cty.Value] {
	return NewPromise(addr, func(self executor) (cty.Value, tfdiags.Diagnostics) {
		var diags tfdiags.Diagnostics

		// From tofu/evaluate.go
		// During the validate walk, input variables are always unknown so
		// that we are validating the configuration for all possible input values
		// rather than for a specific set. Checking against a specific set of
		// input values then happens during the plan walk.
		//
		// This is important because otherwise the validation walk will tend to be
		// overly strict, requiring expressions throughout the configuration to
		// be complicated to accommodate all possible inputs, whereas returning
		// unknown here allows for simpler patterns like using input values as
		// guards to broadly enable/disable resources, avoid processing things
		// that are disabled, etc. OpenTofu's static validation leans towards
		// being liberal in what it accepts because the subsequent plan walk has
		// more information available and so can be more conservative.
		if scope.op == walkValidate {
			// Ensure variable sensitivity is captured in the validate walk
			if config.Sensitive {
				return cty.UnknownVal(config.Type).Mark(marks.Sensitive), diags
			}
			return cty.UnknownVal(config.Type), diags
		}

		var parentEvalCtx tofu.EvalContext

		if scope.op == walkPlan {
			configAddr := addr.Variable.InModule(addr.Module.Module())
			if checkState := scope.Checks; checkState.ConfigHasChecks(configAddr) {
				checkState.ReportCheckableObject(configAddr, addr)
			}
		}

		evalCtx := scope.EvalContext(self, DataOverride{
			addr: addr.Variable,
			value: func() (cty.Value, tfdiags.Diagnostics) {
				return parentEvalCtx.GetVariableValue(addr), nil
			},
		})

		if addr.Module.IsRoot() {
			parentEvalCtx = evalCtx
			input := &tofu.NodeRootVariable{
				Addr:     addr.Variable,
				Config:   config,
				RawValue: input.value,
			}
			diags = input.Execute(ctx, evalCtx, tofu.WalkOperation(scope.op))
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
		} else {
			parentEvalCtx = parentScope.EvalContext(self)
			// It is evaluated in the "parent" module
			input := &tofu.NodeModuleVariable{
				Addr:           addr,
				Config:         config,
				Expr:           input.expr,
				ModuleInstance: addr.Module,
			}
			diags = input.Execute(ctx, parentEvalCtx, tofu.WalkOperation(scope.op))
			if diags.HasErrors() {
				return cty.NilVal, diags
			}

			// HACK: Shift value from parent to child context (see mock hack)
			_, call := addr.Module.CallInstance()
			evalCtx.SetModuleCallArgument(
				call, addr.Variable,
				parentEvalCtx.GetVariableValue(addr),
			)
		}

		// It is evaluated in the "child" module
		ref := &tofu.NodeVariableReferenceInstance{
			Addr:   addr,
			Config: config,
			Expr:   input.expr,

			// TODO VariableFromRemoteModule: c.IsModuleCallFromRemoteModule(callConfig.Name),
		}
		execDiags := ref.Execute(ctx, evalCtx, tofu.WalkOperation(scope.op))
		diags = diags.Append(execDiags)
		return parentEvalCtx.GetVariableValue(addr), diags
	})
}
