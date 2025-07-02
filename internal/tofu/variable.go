package tofu

import (
	"context"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/lang/marks"
	"github.com/opentofu/opentofu/internal/tfdiags"

	"github.com/zclconf/go-cty/cty"
)

type VariableInput struct {
	value *InputValue
	expr  hcl.Expression
}
type VariableInputs map[addrs.InputVariable]VariableInput

func NewRootVariableInputs(values InputValues) VariableInputs {
	inputs := VariableInputs{}
	for name, value := range values {
		inputs[addrs.InputVariable{Name: name}] = VariableInput{value: value}
	}
	return inputs
}

func NewVariable(ctx context.Context, addr addrs.AbsInputVariableInstance, config *configs.Variable, input VariableInput, scope *Scope, parentScope *Scope) *Promise[cty.Value] {
	if scope.op == walkValidate {
		return NewPromise(Ident{base: addr}, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
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
			// Ensure variable sensitivity is captured in the validate walk
			if config.Sensitive {
				return cty.UnknownVal(config.Type).Mark(marks.Sensitive), nil
			}
			return cty.UnknownVal(config.Type), nil
		})
	}

	valuePromise := NewPromise(Ident{addr, "(value)"}, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
		if addr.Module.IsRoot() {
			input := &NodeRootVariable{
				Addr:     addr.Variable,
				Config:   config,
				RawValue: input.value,
			}

			evalCtx, diags := scope.LegacyExecute(ctx, self, input)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
			return evalCtx.GetVariableValue(addr), diags
		} else {
			input := &nodeModuleVariable{
				Addr:           addr,
				Config:         config,
				Expr:           input.expr,
				ModuleInstance: addr.Module,
			}
			evalCtx, diags := parentScope.LegacyExecute(ctx, self, input)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
			return evalCtx.GetVariableValue(addr), diags
		}
	})

	return NewPromise(Ident{addr, "(validate)"}, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
		// Retrieve the value
		val, diags := valuePromise.Value(self)
		if diags.HasErrors() {
			return val, diags
		}

		if scope.op == walkPlan {
			// Register checks if nessesary, apply assumes they already exist
			configAddr := addr.Variable.InModule(addr.Module.Module())
			if checkState := scope.Checks; checkState.ConfigHasChecks(configAddr) {
				checkState.ReportCheckableObject(configAddr, addr)
			}
		}

		// It is evaluated in the "child" module
		ref := &NodeVariableReferenceInstance{
			Addr:   addr,
			Config: config,
			Expr:   input.expr,

			// TODO VariableFromRemoteModule: c.IsModuleCallFromRemoteModule(callConfig.Name),
		}

		evalCtx := scope.EvalContext(self, DataOverride{
			addr: addr.Variable,
			value: func() (cty.Value, tfdiags.Diagnostics) {
				return val, nil
			},
		})

		if !addr.Module.IsRoot() {
			// HACK: Shift value from parent to child context (see mock hack)
			_, call := addr.Module.CallInstance()
			evalCtx.SetModuleCallArgument(call, addr.Variable, val)
		}

		// TODO this does not check references before executing
		// This is probably safe for variables, but should be avoided where possible
		// (&NodeVariableReference{Config: config}).References(),

		diags = ref.Execute(ctx, evalCtx, scope.op)
		return val, diags
	})
}
