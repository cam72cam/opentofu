package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/instances"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

type evalData struct {
	caller    promise
	instance  instances.RepetitionData
	variables map[addrs.InputVariable]*Promise[cty.Value]
	locals    map[addrs.LocalValue]*Promise[cty.Value]
	resources map[addrs.Resource]*Promise[cty.Value]
	calls     map[addrs.ModuleCall]*Promise[cty.Value]
	outputs   map[addrs.OutputValue]*Promise[cty.Value]
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
func (d *evalData) GetResource(addr addrs.Resource, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.resources[addr].Value(d.caller)
}
func (d *evalData) GetLocalValue(addr addrs.LocalValue, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.locals[addr].Value(d.caller)
}
func (d *evalData) GetModule(addr addrs.ModuleCall, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.calls[addr].Value(d.caller)
}
func (d *evalData) GetPathAttr(addr addrs.PathAttr, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	panic("TODO")
}
func (d *evalData) GetTerraformAttr(addr addrs.TerraformAttr, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	panic("TODO")
}
func (d *evalData) GetInputVariable(addr addrs.InputVariable, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.variables[addr].Value(d.caller)
}
func (d *evalData) GetOutput(addr addrs.OutputValue, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.outputs[addr].Value(d.caller)
}
func (d *evalData) GetCheckBlock(addr addrs.Check, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	panic("TODO")
}
