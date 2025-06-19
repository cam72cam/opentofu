package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/didyoumean"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

type DataOverride struct {
	addr  any
	value func() (cty.Value, tfdiags.Diagnostics)
}

type evalData struct {
	caller    executor
	instance  instances.RepetitionData
	workspace string
	overrides []DataOverride

	ModuleData
}

func (d *evalData) StaticValidateReferences(refs []*addrs.Reference, self addrs.Referenceable, source addrs.Referenceable) tfdiags.Diagnostics {
	// TODO
	return nil
}

func (d *evalData) GetCountAttr(addr addrs.CountAttr, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.instance.CountIndex, nil
}
func (d *evalData) GetForEachAttr(addr addrs.ForEachAttr, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	switch addr.Name {
	case "key":
		return d.instance.EachKey, nil
	case "value":
		return d.instance.EachValue, nil
	default:
		panic("impossible")
	}
}

func (d *evalData) override(addr any) (bool, cty.Value, tfdiags.Diagnostics) {
	for _, override := range d.overrides {
		if override.addr == addr {
			val, diags := override.value()
			return true, val, diags
		}
	}
	return false, cty.NilVal, nil
}

// Most of these functions pulled code from internal/tofu/evaluate for diagnostic consistency

func moduleDisplayAddr(addr addrs.ModuleInstance) string {
	switch {
	case addr.IsRoot():
		return "the root module"
	default:
		return addr.String()
	}
}

func (d *evalData) GetResource(addr addrs.Resource, rng tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	promise := d.Resources[addr]
	if promise == nil {
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  `Reference to undeclared resource`,
			Detail:   fmt.Sprintf(`A resource %q %q has not been declared in %s`, addr.Type, addr.Name, moduleDisplayAddr(d.Addr)),
			Subject:  rng.ToHCL().Ptr(),
		})
		return cty.DynamicVal, diags
	}
	if override, val, diags := d.override(addr); override {
		return val, diags
	}
	return promise.Value(d.caller)
}
func (d *evalData) GetLocalValue(addr addrs.LocalValue, rng tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	promise := d.Locals[addr]

	if promise == nil {
		var suggestions []string
		for k := range d.Locals {
			suggestions = append(suggestions, k.Name)
		}
		suggestion := didyoumean.NameSuggestion(addr.Name, suggestions)
		if suggestion != "" {
			suggestion = fmt.Sprintf(" Did you mean %q?", suggestion)
		}

		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  `Reference to undeclared local value`,
			Detail:   fmt.Sprintf(`A local value with the name %q has not been declared.%s`, addr.Name, suggestion),
			Subject:  rng.ToHCL().Ptr(),
		})
		return cty.DynamicVal, diags
	}

	return promise.Value(d.caller)
}
func (d *evalData) GetModule(addr addrs.ModuleCall, rng tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	promise := d.Calls[addr]
	if promise == nil {
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  `Reference to undeclared module`,
			Detail:   fmt.Sprintf(`The configuration contains no %s.`, addr),
			Subject:  rng.ToHCL().Ptr(),
		})
		return cty.DynamicVal, diags
	}

	return promise.Value(d.caller)
}
func (d *evalData) GetPathAttr(addr addrs.PathAttr, rng tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	switch addr.Name {

	case "cwd", "root":
		wd, err := os.Getwd()
		if err != nil {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  `Failed to get working directory`,
				Detail:   fmt.Sprintf(`The value for path.cwd cannot be determined due to a system error: %s`, err),
				Subject:  rng.ToHCL().Ptr(),
			})
			return cty.DynamicVal, diags
		}
		// The current working directory should always be absolute, whether we
		// just looked it up or whether we were relying on ContextMeta's
		// (possibly non-normalized) path.
		wd, err = filepath.Abs(wd)
		if err != nil {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  `Failed to get working directory`,
				Detail:   fmt.Sprintf(`The value for path.cwd cannot be determined due to a system error: %s`, err),
				Subject:  rng.ToHCL().Ptr(),
			})
			return cty.DynamicVal, diags
		}

		return cty.StringVal(filepath.ToSlash(wd)), diags

	case "module":
		return cty.StringVal(filepath.ToSlash(d.SourceDir)), diags

	default:
		suggestion := didyoumean.NameSuggestion(addr.Name, []string{"cwd", "module", "root"})
		if suggestion != "" {
			suggestion = fmt.Sprintf(" Did you mean %q?", suggestion)
		}
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  `Invalid "path" attribute`,
			Detail:   fmt.Sprintf(`The "path" object does not have an attribute named %q.%s`, addr.Name, suggestion),
			Subject:  rng.ToHCL().Ptr(),
		})
		return cty.DynamicVal, diags
	}
}
func (d *evalData) GetTerraformAttr(addr addrs.TerraformAttr, rng tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	switch addr.Name {
	case "workspace":
		return cty.StringVal(d.workspace), diags

	case "env":
		// Prior to Terraform 0.12 there was an attribute "env", which was
		// an alias name for "workspace". This was deprecated and is now
		// removed.
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("Invalid %q attribute", addr.Alias),
			Detail:   fmt.Sprintf(`The %s.env attribute was deprecated in v0.10 and removed in v0.12. The "state environment" concept was renamed to "workspace" in v0.12, and so the workspace name can now be accessed using the %s.workspace attribute.`, addr.Alias, addr.Alias),
			Subject:  rng.ToHCL().Ptr(),
		})
		return cty.DynamicVal, diags

	default:
		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  fmt.Sprintf("Invalid %q attribute", addr.Alias),
			Detail:   fmt.Sprintf(`The %q object does not have an attribute named %q. The only supported attribute is %s.workspace, the name of the currently-selected workspace.`, addr.Alias, addr.Name, addr.Alias),
			Subject:  rng.ToHCL().Ptr(),
		})
		return cty.DynamicVal, diags
	}
}
func (d *evalData) GetInputVariable(addr addrs.InputVariable, rng tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	promise := d.Variables[addr]
	if promise == nil {
		var suggestions []string
		for k := range d.Variables {
			suggestions = append(suggestions, k.Name)
		}
		suggestion := didyoumean.NameSuggestion(addr.Name, suggestions)
		if suggestion != "" {
			suggestion = fmt.Sprintf(" Did you mean %q?", suggestion)
		} else {
			suggestion = fmt.Sprintf(" This variable can be declared with a variable %q {} block.", addr.Name)
		}

		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  `Reference to undeclared input variable`,
			Detail:   fmt.Sprintf(`An input variable with the name %q has not been declared.%s`, addr.Name, suggestion),
			Subject:  rng.ToHCL().Ptr(),
		})
		return cty.DynamicVal, diags
	}

	if override, val, diags := d.override(addr); override {
		return val, diags
	}

	return promise.Value(d.caller)
}
func (d *evalData) GetOutput(addr addrs.OutputValue, rng tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	promise := d.Outputs[addr]
	if promise == nil {
		var suggestions []string
		for k := range d.Outputs {
			suggestions = append(suggestions, k.Name)
		}
		suggestion := didyoumean.NameSuggestion(addr.Name, suggestions)
		if suggestion != "" {
			suggestion = fmt.Sprintf(" Did you mean %q?", suggestion)
		}

		diags = diags.Append(&hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  `Reference to undeclared output value`,
			Detail:   fmt.Sprintf(`An output value with the name %q has not been declared.%s`, addr.Name, suggestion),
			Subject:  rng.ToHCL().Ptr(),
		})
		return cty.DynamicVal, diags
	}
	return promise.Value(d.caller)
}
func (d *evalData) GetCheckBlock(addr addrs.Check, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	// TODO
	return cty.DynamicVal, nil
}
