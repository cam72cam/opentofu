package command

import (
	"log"

	"github.com/hashicorp/hcl/v2"
	"github.com/opentofu/opentofu/internal/command/arguments"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/tfdiags"
)

// StateEncryptionOpts are the options used to initialize a state_encryption.StateEncryption.
type StateEncryptionOpts struct {
	// Config is a representation of the state_encryption configuration block given in
	// the root module, or nil if no such block is present.
	Config *configs.StateEncryption

	// ConfigOverride is an hcl.Body that, if non-nil, will be used with
	// configs.MergeBodies to override the type-specific state_encryption configuration
	// arguments in Config.
	ConfigOverride hcl.Body

	// ViewType will set console output format for the
	// initialization operation (JSON or human-readable).
	ViewType arguments.ViewType
}

// StateEncryption initializes the state_encryption for this CLI session.
func (m *Meta) StateEncryption(opts *StateEncryptionOpts) tfdiags.Diagnostics {
	var diags tfdiags.Diagnostics

	// If no opts are set, then initialize
	if opts == nil {
		opts = &StateEncryptionOpts{}
	}

	// Initialize a state_encryption from the config unless we're forcing a purely
	// local operation.
	var state_encryptionDiags tfdiags.Diagnostics
	cfg, state_encryptionDiags = m.state_encryptionConfig(opts)
	diags = diags.Append(state_encryptionDiags)
	if diags.HasErrors() {
		return diags
	}

	// TODO this is where we pass the cfg.Config into the state encryption package (can be nil)

	return diags
}

// state_encryptionCLIOpts returns a state_encryption.CLIOpts object that should be passed to
// a state_encryption that supports local CLI operations.
func (m *Meta) state_encryptionCLIOpts() (*state_encryption.CLIOpts, error) {
	contextOpts, err := m.contextOpts()
	if contextOpts == nil && err != nil {
		return nil, err
	}
	return &state_encryption.CLIOpts{
		CLI:                 m.Ui,
		CLIColor:            m.Colorize(),
		Streams:             m.Streams,
		StatePath:           m.statePath,
		StateOutPath:        m.stateOutPath,
		StateBackupPath:     m.backupPath,
		ContextOpts:         contextOpts,
		Input:               m.Input(),
		RunningInAutomation: m.RunningInAutomation,
	}, err
}

// state_encryptionConfig returns the local configuration for the state_encryption
func (m *Meta) state_encryptionConfig(opts *StateEncryptionOpts) (*configs.StateEncryption, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics

	if opts.Config == nil {
		// check if the config was missing, or just not required
		conf, moreDiags := m.loadStateEncryptionConfig(".")
		diags = diags.Append(moreDiags)
		if moreDiags.HasErrors() {
			return nil, diags
		}

		if conf == nil {
			log.Println("[TRACE] Meta.StateEncryption: no config given or present on disk, so returning nil config")
			return nil, nil
		}

		log.Printf("[TRACE] Meta.StateEncryption: StateEncryptionOpts.Config not set, so using settings loaded from %s", conf.DeclRange)
		opts.Config = conf
	}

	c := opts.Config

	if c == nil {
		log.Println("[TRACE] Meta.StateEncryption: no explicit state_encryption config, so returning nil config")
		return nil, nil
	}

	configBody := c.Config

	// If we have an override configuration body then we must apply it now.
	if opts.ConfigOverride != nil {
		log.Println("[TRACE] Meta.StateEncryption: merging -state_encryption-config=... CLI overrides into state_encryption configuration")
		configBody = configs.MergeBodies(configBody, opts.ConfigOverride)
	}

	// We'll shallow-copy configs.StateEncryption here so that we can replace the
	// body without affecting others that hold this reference.
	configCopy := *c
	configCopy.Config = configBody
	return &configCopy, diags
}
