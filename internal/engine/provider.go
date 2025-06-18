package engine

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/addrs"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/providers"
	"github.com/opentofu/opentofu/internal/tfdiags"
	"github.com/opentofu/opentofu/internal/tofu"
	"github.com/zclconf/go-cty/cty"
)

type AbsProviderConfig struct {
	Module addrs.ModuleInstance
	Local  addrs.LocalProviderConfig
}

func (pc AbsProviderConfig) String() string {
	return fmt.Sprintf("%s.%s", pc.Module, pc.Local)
}

type Provider func(self executor) (providers.Interface, func(), tfdiags.Diagnostics)

func NewProvider(ctx context.Context, addr AbsProviderConfig, providerType addrs.Provider, config *configs.Provider, scope *Scope) Provider {
	// TODO provider instances

	// reworked from tofu/node_provider.go
	// This breaks non-direct provider inputs?
	configBody := tofu.BuildProviderConfig(&tofu.MockEvalContext{}, addrs.AbsProviderConfig{Provider: providerType, Module: addr.Module.Module(), Alias: addr.Local.Alias}, config)

	cfgVal := NewPromise(addr, func(self executor) (cty.Value, tfdiags.Diagnostics) {
		log.Printf("[TRACE] building configuration for provider %s", addr)
		evalCtx := scope.EvalContext(self)

		provider, done, diags := scope.Plugins.ConfiguredProvider(providerType, cty.NilVal)
		defer done()

		resp := provider.GetProviderSchema(ctx)
		bodyDiags := resp.Diagnostics.InConfigBody(configBody, addr.String()) // TODO addr.InstanceString(providerKey))
		diags = diags.Append(bodyDiags)
		if diags.HasErrors() {
			return cty.NilVal, diags
		}

		configSchema := resp.Provider.Block
		data := tofu.EvalDataForNoInstanceKey
		/*if n.Config != nil && n.Config.Instances != nil {
			data = n.Config.Instances[providerKey]
		}*/

		configVal, configBody, evalDiags := evalCtx.EvaluateBlock(configBody, configSchema, nil, data)
		diags = diags.Append(evalDiags)
		if evalDiags.HasErrors() {
			return cty.NilVal, diags
		}

		verifyConfigIsKnown := scope.op == walkImport
		if verifyConfigIsKnown && !configVal.IsWhollyKnown() {
			diags = diags.Append(&hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid provider configuration",
				Detail:   fmt.Sprintf("The configuration for %s depends on values that cannot be determined until apply.", addr),
				Subject:  &config.DeclRange,
			})
			return cty.NilVal, diags
		}

		// If our config value contains any marked values, ensure those are
		// stripped out before sending this to the provider
		unmarkedConfigVal, _ := configVal.UnmarkDeep()

		// Allow the provider to validate and insert any defaults into the full
		// configuration.
		req := providers.ValidateProviderConfigRequest{
			Config: unmarkedConfigVal,
		}

		// ValidateProviderConfig is only used for validation. We are intentionally
		// ignoring the PreparedConfig field to maintain existing behavior.
		validateResp := provider.ValidateProviderConfig(ctx, req)
		diags = diags.Append(validateResp.Diagnostics.InConfigBody(configBody, addr.String())) //addr.InstanceString(providerKey)))
		if diags.HasErrors() && config == nil {
			// If there isn't an explicit "provider" block in the configuration,
			// this error message won't be very clear. Add some detail to the error
			// message in this case.
			diags = diags.Append(tfdiags.Sourceless(
				tfdiags.Error,
				"Invalid provider configuration",
				fmt.Sprintf(providerConfigErr, provider),
			))
		}

		if diags.HasErrors() {
			return cty.NilVal, diags
		}

		// If the provider returns something different, log a warning to help
		// indicate to provider developers that the value is not used.
		preparedCfg := validateResp.PreparedConfig
		if preparedCfg != cty.NilVal && !preparedCfg.IsNull() && !preparedCfg.RawEquals(unmarkedConfigVal) {
			log.Printf("[WARN] ValidateProviderConfig from %q changed the config value, but that value is unused", addr)
		}

		return unmarkedConfigVal, diags

	})

	return func(self executor) (providers.Interface, func(), tfdiags.Diagnostics) {
		cfg, diags := cfgVal.Value(self)
		diags = diags.InConfigBody(configBody, addr.String()) //addr.InstanceString(providerKey)))
		if diags.HasErrors() {
			if config == nil {
				// If there isn't an explicit "provider" block in the configuration,
				// this error message won't be very clear. Add some detail to the error
				// message in this case.
				diags = diags.Append(tfdiags.Sourceless(
					tfdiags.Error,
					"Invalid provider configuration",
					fmt.Sprintf(providerConfigErr, providerType),
				))
			}
			return nil, nil, diags
		}

		if scope.op == walkValidate {
			return scope.Plugins.ConfiguredProvider(providerType, cty.NilVal)
		} else {
			return scope.Plugins.ConfiguredProvider(providerType, cfg)
		}
	}
}

const providerConfigErr = `Provider %q requires explicit configuration. Add a provider block to the root module and configure the provider's required arguments as described in the provider documentation.
`
