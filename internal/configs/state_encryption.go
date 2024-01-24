package configs

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
)

const (
	StateEncryptionKeyBackend     = "backend"
	StateEncryptionKeyStateFile   = "statefile"
	StateEncryptionKeyPlanFile    = "planfile"
	StateEncryptionKeyRemoteState = "remote_state"
)

var stateEncryptionMapSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{Type: StateEncryptionKeyBackend},
		{Type: StateEncryptionKeyStateFile},
		{Type: StateEncryptionKeyPlanFile},
		{
			Type:       StateEncryptionKeyRemoteState,
			LabelNames: []string{"name"},
		},
	},
}

type StateEncryptionMap struct {
	Configs   map[string]*StateEncryption
	DeclRange hcl.Range
}

func decodeStateEncryptionMap(block *hcl.Block) (*StateEncryptionMap, hcl.Diagnostics) {
	return LoadStateEncryptionMap(block.Body, block.DefRange)
}
func LoadStateEncryptionMap(body hcl.Body, rng hcl.Range) (*StateEncryptionMap, hcl.Diagnostics) {
	content, diags := body.Content(stateEncryptionMapSchema)
	if diags.HasErrors() {
		return nil, diags
	}

	configs := make(map[string]*StateEncryption)

	// Pull out standard blocks
	for _, block := range content.Blocks {
		ident := block.Type

		if block.Type == StateEncryptionKeyRemoteState {
			// TODO helper function
			ident = fmt.Sprintf("%s:%s", ident, block.Labels[0])
		}

		if _, ok := configs[ident]; ok {
			// ERROR, duplicate key, probably parse error
			continue
		}

		cfg, cfgDiags := decodeStateEncryptionBlock(block)
		diags = append(diags, cfgDiags...)
		if cfgDiags.HasErrors() {
			continue
		}

		configs[ident] = cfg
	}

	return &StateEncryptionMap{
		Configs:   configs,
		DeclRange: rng,
	}, diags
}

func (s *StateEncryptionMap) Merge(override *StateEncryptionMap) {
	for key, block := range override.Configs {
		if exist, ok := s.Configs[key]; ok {
			exist.Merge(block)
		} else {
			s.Configs[key] = block
		}
	}
}

type StateEncryption struct {
	Required bool

	KeyProvider      hcl.Body
	KeyProviderRange hcl.Range

	Method      hcl.Body
	MethodRange hcl.Range

	Fallback *StateEncryption

	DeclRange hcl.Range
}

const (
	StateEncryptionBlockKeyProvider = "key_provider"
	StateEncryptionBlockKeyMethod   = "method"
	StateEncryptionBlockKeyFallback = "fallback"
	StateEncryptionBlockKeyRequired = "required"
)

var stateEncryptionBlockSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{
			Type:       StateEncryptionBlockKeyProvider,
			LabelNames: []string{"name"},
		},
		{
			Type:       StateEncryptionBlockKeyMethod,
			LabelNames: []string{"name"},
		},
		{
			Type: StateEncryptionBlockKeyFallback,
		},
	},
}

func decodeStateEncryptionBlock(block *hcl.Block) (*StateEncryption, hcl.Diagnostics) {
	content, diags := block.Body.Content(stateEncryptionBlockSchema)
	if diags.HasErrors() {
		return nil, diags
	}

	cfg := StateEncryption{}

	for _, block := range content.Blocks {
		switch block.Type {
		case StateEncryptionBlockKeyProvider:
			if cfg.KeyProvider != nil {
				panic("Error duplicate, this should be a diags error")
			}
			cfg.KeyProvider = block.Body
			cfg.KeyProviderRange = block.DefRange
		case StateEncryptionBlockKeyMethod:
			if cfg.Method != nil {
				panic("Error duplicate, this should be a diags error")
			}
			cfg.Method = block.Body
			cfg.MethodRange = block.DefRange
		case StateEncryptionBlockKeyFallback:
			if cfg.Fallback != nil {
				panic("Error duplicate, this should be a diags error")
			}
			fallback, fallbackDiags := decodeStateEncryptionBlock(block)
			diags = append(diags, fallbackDiags...)
			cfg.Fallback = fallback
		}
	}

	if diags.HasErrors() {
		return nil, diags
	}

	return &cfg, diags
}

func (c *StateEncryption) Merge(override *StateEncryption) {
	c.Required = c.Required || override.Required

	if override.KeyProvider != nil {
		if c.KeyProvider == nil {
			c.KeyProvider = override.KeyProvider
			c.KeyProviderRange = override.KeyProviderRange
		} else {
			c.KeyProvider = MergeBodies(c.KeyProvider, override.KeyProvider)
		}
	}

	if override.Method != nil {
		if c.Method == nil {
			c.Method = override.Method
			c.MethodRange = override.MethodRange
		} else {
			c.Method = MergeBodies(c.Method, override.Method)
		}
	}

	if override.Fallback != nil {
		if c.Fallback == nil {
			c.Fallback = override.Fallback
		} else {
			c.Fallback.Merge(override)
		}
	}
}
