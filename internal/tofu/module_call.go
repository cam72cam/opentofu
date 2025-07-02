package tofu

import (
	"context"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/tfdiags"
	
	"github.com/zclconf/go-cty/cty"
)

func getModuleCallInputExpressions(config *configs.ModuleCall, moduleConfig *configs.Config) (map[string]hcl.Expression, tfdiags.Diagnostics) {
	// Lifted from transform_module_variable

	// We need to construct a schema for the expected call arguments based on
	// the configured variables in our config, which we can then use to
	// decode the content of the call block.
	schema := &hcl.BodySchema{}
	for _, v := range moduleConfig.Module.Variables {
		schema.Attributes = append(schema.Attributes, hcl.AttributeSchema{
			Name:     v.Name,
			Required: v.Default == cty.NilVal,
		})
	}

	content, contentDiags := config.Config.Content(schema)
	diags := tfdiags.Diagnostics{}.Append(contentDiags)
	if diags.HasErrors() {
		// Validation code elsewhere should deal with any errors before we
		// get in here, but we'll report them out here just in case, to
		// avoid crashes.
		return nil, diags
	}

	exprs := map[string]hcl.Expression{}

	for name, attr := range content.Attributes {
		exprs[name] = attr.Expr
	}

	return exprs, diags

}

type ModuleInstances map[addrs.InstanceKey]Module
type ModuleCall struct {
	*Promise[cty.Value]
	instances *Promise[ModuleInstances]
}

func NewModuleCall(ctx context.Context, addr addrs.AbsModuleCall, config *configs.ModuleCall, moduleConfig *configs.Config, scope *Scope) ModuleCall {
	if scope.op == walkValidate {
		// Validate only ever does a single expansion
		expansion := NewPromise(Ident{addr, "(expand)"}, func(self *Executor) (ModuleInstances, tfdiags.Diagnostics) {
			node := &NodeValidateModule{NodeExpandModule{
				Addr:       append(addr.Module.Module(), config.Name),
				Config:     moduleConfig.Module,
				ModuleCall: config,
			}}
			_, diags := scope.LegacyExecute(ctx, self, node)
			if diags.HasErrors() {
				return nil, diags
			}

			exprs, exprDiags := getModuleCallInputExpressions(config, moduleConfig)
			diags = diags.Append(exprDiags)

			input := VariableInputs{}
			for _, v := range moduleConfig.Module.Variables {
				input[addrs.InputVariable{Name: v.Name}] = VariableInput{
					expr: exprs[v.Name], // TODO this differs from existing tofu logic
				}
			}

			return ModuleInstances{addrs.NoKey: NewModule(ctx, addr.Instance(addrs.NoKey), moduleConfig, input, scope)}, diags
		})

		outputValue := NewPromise(Ident{addr, "(call)"}, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
			expanded, diags := expansion.Value(self)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
			out, diags := expanded[addrs.NoKey].Value(self)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}

			// FROM: tofu/evaluate.go
			ty := out.Type()
			switch {
			case config.Count != nil:
				return cty.UnknownVal(cty.List(ty)), nil
			case config.ForEach != nil:
				return cty.UnknownVal(cty.Map(ty)), nil
			default:
				return cty.UnknownVal(ty), nil
			}
		})

		return ModuleCall{outputValue, expansion}
	}

	expansion := NewPromise(Ident{addr, "(expand)"}, func(self *Executor) (ModuleInstances, tfdiags.Diagnostics) {
		var diags tfdiags.Diagnostics

		switch {
		case config.Count != nil:
			evalCtx := scope.EvalContext(self)
			if diags.HasErrors() {
				return nil, diags
			}
			count, ctDiags := EvaluateCountExpression(config.Count, evalCtx, addr.Module)
			diags = diags.Append(ctDiags)
			if diags.HasErrors() {
				return nil, diags
			}
			scope.expander.SetModuleCount(addr.Module, addr.Call, count)

		case config.ForEach != nil:
			evalCtx := scope.EvalContext(self)
			forEach, feDiags := EvaluateForEachExpression(config.ForEach, evalCtx, addr.Module)
			diags = diags.Append(feDiags)
			if diags.HasErrors() {
				return nil, diags
			}
			scope.expander.SetModuleForEach(addr.Module, addr.Call, forEach)

		default:
			scope.expander.SetModuleSingle(addr.Module, addr.Call)
		}

		exprs, exprDiags := getModuleCallInputExpressions(config, moduleConfig)
		diags = diags.Append(exprDiags)
		if diags.HasErrors() {
			return nil, diags
		}

		ret := ModuleInstances{}
		for _, modAddr := range scope.expander.ExpandAbsModuleCall(addr) {
			input := VariableInputs{}
			for _, v := range moduleConfig.Module.Variables {
				input[addrs.InputVariable{Name: v.Name}] = VariableInput{
					expr: exprs[v.Name],
				}
			}
			key := modAddr[len(modAddr)-1].InstanceKey
			ret[key] = NewModule(ctx, modAddr, moduleConfig, input, scope)

		}
		return ret, diags
	})

	outputValue := NewPromise(Ident{addr, "(call)"}, func(self *Executor) (cty.Value, tfdiags.Diagnostics) {
		// expansion
		expanded, diags := expansion.Value(self)
		if diags.HasErrors() {
			return cty.NilVal, diags
		}

		instances := make(map[addrs.InstanceKey]cty.Value)
		for key, mod := range expanded {
			instances[key], diags = mod.Value(self)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
		}

		// Lifted from tofu/evaluate.go

		switch {
		case config.Count != nil:
			length := -1
			for key := range instances {
				intKey, ok := key.(addrs.IntKey)
				if !ok {
					// old key from state which is being dropped
					continue
				}
				if int(intKey) >= length {
					length = int(intKey) + 1
				}
			}

			if length <= 0 {
				return cty.EmptyTupleVal, nil

			}
			vals := make([]cty.Value, length)
			for key, instance := range instances {
				intKey, ok := key.(addrs.IntKey)
				if !ok {
					// old key from state which is being dropped
					continue
				}

				vals[int(intKey)] = instance
			}

			// Insert unknown values where there are any missing instances
			for i, v := range vals {
				if v.IsNull() {
					vals[i] = cty.DynamicVal
					continue
				}
			}
			return cty.TupleVal(vals), nil

		case config.ForEach != nil:
			instanceMap := make(map[string]cty.Value)
			for key, mod := range instances {
				sk := key.(addrs.StringKey)
				instanceMap[string(sk)] = mod
			}
			return cty.ObjectVal(instanceMap), nil
		default:
			return instances[addrs.NoKey], nil
		}
	})

	return ModuleCall{outputValue, expansion}
}

func (m ModuleCall) Expand(c *Manager, exec *Executor) {
	if m.instances == nil {
		return
	}
	expanded, _ := m.instances.Value(exec)
	for _, mod := range expanded {
		mod.Collect(c)
	}
}
