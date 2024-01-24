package command

import (
	"os"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/json"
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
	path := "STATE_ENCRYPTION"

	raw := os.Getenv(path)

	if len(raw) == 0 {
		// Undefined
		return nil, nil
	}

	var diags tfdiags.Diagnostics
	var fDiags hcl.Diagnostics
	var file *hcl.File

	if raw[0] == byte('{') {
		file, fDiags = json.Parse([]byte(raw), path)
	} else {
		file, fDiags = hclsyntax.ParseConfig([]byte(raw), path, hcl.Pos{Byte: 0, Line: 1, Column: 1})
	}

	diags = diags.Append(fDiags)

	cfg, cfgDiags := configs.LoadStateEncryptionMap(file.Body, hcl.Range{Filename: path})
	diags = diags.Append(cfgDiags)

	return cfg, diags
}
