package command

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/states/encryption"
	"github.com/opentofu/opentofu/internal/tfdiags"
)

func (m *Meta) StateEncryptionSetup(rootMod *configs.Module) tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics

	base := rootMod.StateEncryption

	for _, item := range *m.stateEncryptionArgs.items {
		file, fDiags := m.loadStateEncryptionFile(item.Value)
		diags = diags.Append(fDiags)
		if file != nil {
			if base == nil {
				base = file
			} else {
				base.Merge(file)
			}
		}
	}

	// TODO
	env, envDiags := m.loadStateEncryptionEnv()
	diags = diags.Append(envDiags)

	if env != nil {
		if base == nil {
			base = env
		} else {
			base.Merge(env)
		}
	}

	if base != nil {
		encryption.SetupSingleton(*base)
	}

	return diags
}

func (m *Meta) loadStateEncryptionFile(path string) (*configs.StateEncryptionMap, tfdiags.Diagnostics) {
	body, diags := m.loadHCLFile(path)
	if diags.HasErrors() {
		return nil, diags
	}

	cfg, cfgDiags := configs.LoadStateEncryptionMap(body, hcl.Range{Filename: path})
	diags = diags.Append(cfgDiags)

	return cfg, diags
}

func (m *Meta) loadStateEncryptionEnv() (*configs.StateEncryptionMap, tfdiags.Diagnostics) {
	return nil, nil //TODO
}
