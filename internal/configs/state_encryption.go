package configs

import "github.com/hashicorp/hcl/v2"

type StateEncryption struct {
	Type   string
	Config hcl.Body

	TypeRange hcl.Range
	DeclRange hcl.Range
}

func decodeStateEncryptionBlock(block *hcl.Block) (*StateEncryption, hcl.Diagnostics) {
	return &StateEncryption{
		Type:      block.Labels[0],
		TypeRange: block.LabelRanges[0],
		Config:    block.Body,
		DeclRange: block.DefRange,
	}, nil
}
