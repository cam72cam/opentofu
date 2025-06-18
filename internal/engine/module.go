package engine

import (
	"context"
	"sync"

	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

type ConcurrencyPool struct {
	wg    sync.WaitGroup
	lock  sync.Mutex
	diags tfdiags.Diagnostics
	pool  chan struct{}
}

func NewConcurrencyPool(size int) *ConcurrencyPool {
	c := make(chan struct{}, size)
	for i := 0; i < size; i++ {
		c <- struct{}{}
	}
	return &ConcurrencyPool{pool: c}
}

func (c *ConcurrencyPool) Add(p ValuePromise) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		slot := <-c.pool
		_, diags := p.Value(nil)
		c.pool <- slot

		c.lock.Lock()
		defer c.lock.Unlock()
		c.diags = c.diags.Append(diags)
	}()
}
func (c *ConcurrencyPool) Expand(e ExpandValuePromise) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		diags := e.Expand(c)
		c.lock.Lock()
		defer c.lock.Unlock()
		c.diags = c.diags.Append(diags)
	}()
}

func (c *ConcurrencyPool) Wait() tfdiags.Diagnostics {
	c.wg.Wait()
	return c.diags
}

type ValuePromise interface {
	Value(executor) (cty.Value, tfdiags.Diagnostics)
}

type ExpandValuePromise interface {
	ValuePromise
	Expand(*ConcurrencyPool) tfdiags.Diagnostics // Could also return map[addrs.InstanceKey]*Scope for DependsOn
}

type ModuleData struct {
	Addr      addrs.ModuleInstance
	SourceDir string
	Variables map[addrs.InputVariable]ValuePromise
	Locals    map[addrs.LocalValue]ValuePromise
	Resources map[addrs.Resource]ExpandValuePromise
	Calls     map[addrs.ModuleCall]ExpandValuePromise
	Outputs   map[addrs.OutputValue]ValuePromise
	Providers map[addrs.LocalProviderConfig]Provider
}

func NewModuleData(addr addrs.ModuleInstance, sourceDir string) ModuleData {
	return ModuleData{
		Addr:      addr,
		SourceDir: sourceDir,
		Variables: map[addrs.InputVariable]ValuePromise{},
		Locals:    map[addrs.LocalValue]ValuePromise{},
		Resources: map[addrs.Resource]ExpandValuePromise{},
		Calls:     map[addrs.ModuleCall]ExpandValuePromise{},
		Outputs:   map[addrs.OutputValue]ValuePromise{},
		Providers: map[addrs.LocalProviderConfig]Provider{},
	}
}

type Module struct {
	ValuePromise
	ModuleData
}

func NewModule(ctx context.Context, addr addrs.ModuleInstance, config *configs.Config, inputs VariableInputs, parentScope *Scope) Module {
	data := NewModuleData(addr, config.Module.SourceDir)
	scope := NewScope(addr, parentScope, data)

	for _, variable := range config.Module.Variables {
		varAddr := addrs.InputVariable{Name: variable.Name}
		data.Variables[varAddr] = NewVariable(ctx, varAddr.Absolute(addr), variable, inputs[varAddr], scope, parentScope)
	}
	for _, local := range config.Module.Locals {
		localAddr := addrs.LocalValue{Name: local.Name}
		data.Locals[localAddr] = NewLocal(ctx, localAddr.Absolute(addr), local, scope)
	}
	for _, resource := range config.Module.ManagedResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		data.Resources[resAddr] = NewResource(ctx, resAddr.Absolute(addr), resource, scope)
	}
	for _, resource := range config.Module.DataResources {
		resAddr := addrs.Resource{Name: resource.Name, Type: resource.Type, Mode: addrs.ManagedResourceMode}
		data.Resources[resAddr] = NewResource(ctx, resAddr.Absolute(addr), resource, scope)
	}
	for _, call := range config.Module.ModuleCalls {
		callAddr := addrs.ModuleCall{Name: call.Name}
		data.Calls[callAddr] = NewModuleCall(ctx, callAddr.Absolute(addr), call, config.Children[call.Name], scope)
	}
	for _, output := range config.Module.Outputs {
		outputAddr := addrs.OutputValue{Name: output.Name}
		data.Outputs[outputAddr] = NewOutput(ctx, outputAddr.Absolute(addr), output, scope)
	}
	// Copy in parent providers (legacy)
	for addr, provider := range parentScope.Data.Providers {
		data.Providers[addr] = provider
	}
	// Link in provider requirements
	if addr.IsRoot() {
		// Unconfigured providers
		// TODO this does not handle local names well
		reqs, _, _ := config.ProviderRequirements()
		for provider := range reqs {
			providerAddr := addrs.LocalProviderConfig{LocalName: provider.Type}
			data.Providers[providerAddr] = NewProvider(ctx, AbsProviderConfig{Module: addr, Local: providerAddr}, provider, nil, scope)
			// TODO if we are validating, create a stub
		}
	} else {
		for _, provider := range config.Module.ProviderRequirements.RequiredProviders {
			// Linked from parent call
			linkParentProvider := func(alias string) {
				//TODO
			}
			linkParentProvider("")
			for _, alias := range provider.Aliases {
				linkParentProvider(alias.Alias)
			}
		}
	}
	// Explicitly declared provider configs within this module
	for _, provider := range config.Module.ProviderConfigs {
		providerAddr := addrs.LocalProviderConfig{LocalName: provider.Name, Alias: provider.Alias}
		data.Providers[providerAddr] = NewProvider(ctx, AbsProviderConfig{Module: addr, Local: providerAddr}, config.Module.ProviderForLocalConfig(providerAddr), provider, scope)
	}

	return Module{
		NewPromise(Ident{addr, "(outputs)"}, func(self executor) (cty.Value, tfdiags.Diagnostics) {
			obj := map[string]cty.Value{}
			var diags tfdiags.Diagnostics
			for name := range config.Module.Outputs {
				outVal, outDiags := data.Outputs[addrs.OutputValue{Name: name}].Value(self)
				obj[name] = outVal
				diags = diags.Append(outDiags)
			}
			return cty.ObjectVal(obj), diags
		}),
		data,
	}
}

func collectDiagnostics[T comparable](m map[T]ValuePromise) tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics
	for _, p := range m {
		_, newDiags := p.Value(nil)
		diags = diags.Append(newDiags)
	}
	return diags
}
func (m *Module) Collect(c *ConcurrencyPool) {
	//diags = diags.Append(collectDiagnostics(m.Variables))
	//diags = diags.Append(collectDiagnostics(m.Locals))
	for _, resource := range m.Resources {
		c.Expand(resource)
	}
	// Follow Expansion
	for _, call := range m.Calls {
		c.Expand(call)
	}
	// Do we only care about the root module here?
	for _, out := range m.Outputs {
		c.Add(out)
	}
}
