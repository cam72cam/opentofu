package engine

import (
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

// Translation between lazy walk and existing scope
type evalPromise func() (cty.Value, tfdiags.Diagnostics)
type evalData struct {
	CountAttrs     map[addrs.CountAttr]evalPromise
	ForEachAttrs   map[addrs.ForEachAttr]evalPromise
	Resources      map[addrs.Resource]evalPromise
	LocalValues    map[addrs.LocalValue]evalPromise
	Modules        map[addrs.ModuleCall]evalPromise
	PathAttrs      map[addrs.PathAttr]evalPromise
	TerraformAttrs map[addrs.TerraformAttr]evalPromise
	InputVariables map[addrs.InputVariable]evalPromise
	Outputs        map[addrs.OutputValue]evalPromise
	CheckBlocks    map[addrs.Check]evalPromise
}

func NewEvalData() *evalData {
	return &evalData{
		CountAttrs:     map[addrs.CountAttr]evalPromise{},
		ForEachAttrs:   map[addrs.ForEachAttr]evalPromise{},
		Resources:      map[addrs.Resource]evalPromise{},
		LocalValues:    map[addrs.LocalValue]evalPromise{},
		Modules:        map[addrs.ModuleCall]evalPromise{},
		PathAttrs:      map[addrs.PathAttr]evalPromise{},
		TerraformAttrs: map[addrs.TerraformAttr]evalPromise{},
		InputVariables: map[addrs.InputVariable]evalPromise{},
		Outputs:        map[addrs.OutputValue]evalPromise{},
		CheckBlocks:    map[addrs.Check]evalPromise{},
	}
}

func (d *evalData) StaticValidateReferences(refs []*addrs.Reference, self addrs.Referenceable, source addrs.Referenceable) tfdiags.Diagnostics {
	// TODO
	return nil
}

func (d *evalData) GetCountAttr(addr addrs.CountAttr, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.CountAttrs[addr]()
}
func (d *evalData) GetForEachAttr(addr addrs.ForEachAttr, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.ForEachAttrs[addr]()
}
func (d *evalData) GetResource(addr addrs.Resource, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.Resources[addr]()
}
func (d *evalData) GetLocalValue(addr addrs.LocalValue, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.LocalValues[addr]()
}
func (d *evalData) GetModule(addr addrs.ModuleCall, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.Modules[addr]()
}
func (d *evalData) GetPathAttr(addr addrs.PathAttr, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.PathAttrs[addr]()
}
func (d *evalData) GetTerraformAttr(addr addrs.TerraformAttr, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.TerraformAttrs[addr]()
}
func (d *evalData) GetInputVariable(addr addrs.InputVariable, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.InputVariables[addr]()
}
func (d *evalData) GetOutput(addr addrs.OutputValue, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.Outputs[addr]()
}
func (d *evalData) GetCheckBlock(addr addrs.Check, _ tfdiags.SourceRange) (cty.Value, tfdiags.Diagnostics) {
	return d.CheckBlocks[addr]()
}
