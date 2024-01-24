package encryption

import (
	"fmt"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/opentofu/opentofu/internal/configs"
	"github.com/opentofu/opentofu/internal/states/encryption/encryptionflow"
)

// Encryption is the main interface to feed the encryption configuration and obtain the encryptionflow.Flow for running
// the actual encryption.
//
// Note: a large portion of the OpenTofu codebase is still procedural, which means there is no way to properly inject
// the Encryption object and carry the information it holds across subsystem boundaries. In most cases you should use
// GetSingleton() to get a globally scoped copy of the Encryption object. However, for tests you should use the New()
// function and hopefully, some time in the future, we can get rid of the singleton entirely.
type Encryption interface {
	// RemoteState returns an encryption flow suitable for the remote state of the current project.
	//
	// When implementing this interface:
	//
	// - If the user provided no configuration, this function must return a flow that passes through the data
	//   unmodified.
	// - If the user only provided an environment configuration with the key encryptionconfig.KeyDefaultRemote, the
	//   returned flow should use this configuration.
	// - If the user provided a non-default HCL or environment configuration, these configurations should be merged
	//   with the environment taking precedence. The default configuration should be ignored.
	//
	// Please note, the encryption and decryption fallback configuration may have separate configuration. This method
	// should support this scenario to allow for encryption rollover.
	//
	// Tip: encryptionconfig.ConfigMap.Merge implements these precedence rules.
	RemoteState() (encryptionflow.StateFlow, error)

	// RemoteStateDatasource returns an encryption flow suitable for the remote state of a remote state data source.
	// You should pass the remote state data source name as follows:
	//
	//    encryptionconfig.Key("terraform_remote_state.foo")
	//
	// For indexed resources, please pass the index as follows:
	//
	//    encryptionconfig.Key("terraform_remote_state.foo[42]")
	//    encryptionconfig.Key("terraform_remote_state.foo[test]")
	//
	// See encryptionconfig.Key for more details on the key format.
	//
	// When implementing this interface:
	//
	// - If the user provided no configuration, this function must return a flow that passes through the data
	//   unmodified.
	// - If the user only provided an environment configuration with the key encryptionconfig.KeyDefaultRemote, the
	//   returned flow should use this configuration.
	// - If the user provided a non-default HCL or environment configuration, these configurations should be merged
	//   with the environment taking precedence. The default configuration should be ignored.
	//
	// Please note, the encryption and decryption fallback configuration may have separate configuration. This method
	// should support this scenario to allow for encryption rollover.
	//
	// Tip: encryptionconfig.ConfigMap.Merge implements these precedence rules.
	RemoteStateDatasource(configKey string, def *configs.StateEncryption) (encryptionflow.StateFlow, error)

	// StateFile returns an encryption flow suitable for encrypting the state file.
	//
	// When implementing this interface:
	//
	// - If the user provided no configuration, this function must return a flow that passes through the data
	//   unmodified.
	// - The default configuration is always ignored in this case because it is only the default for remote states.
	// - If the user provided a non-default HCL or environment configuration, these configurations should be merged
	//   with the environment taking precedence. The default configuration should be ignored.
	//
	// Please note, the encryption and decryption fallback configuration may have separate configuration. This method
	// should support this scenario to allow for encryption rollover.
	//
	// Tip: encryptionconfig.ConfigMap.Merge implements these precedence rules.
	StateFile() (encryptionflow.StateFlow, error)

	// PlanFile returns an encryption flow suitable for encrypting the plan file.
	//
	// When implementing this interface:
	//
	// - If the user provided no configuration, this function must return a flow that passes through the data
	//   unmodified.
	// - The default configuration is always ignored in this case because it is only the default for remote states.
	// - If the user provided a non-default HCL or environment configuration, these configurations should be merged
	//   with the environment taking precedence. The default configuration should be ignored.
	//
	// Tip: encryptionconfig.ConfigMap.Merge implements these precedence rules.
	PlanFile() (encryptionflow.PlanFlow, error)
}

type encryption struct {
	configs map[string]*configs.StateEncryption
	mutex   sync.Mutex
	logger  hclog.Logger
}

// New creates a new Encryption object. You can use this object to feed in encryption configuration and then create
// an encryptionflow.StateFlow or encryptionflow.PlanFlow as needed.
//
// Note: a large portion of the OpenTofu codebase is still procedural, which means there is no way to properly inject
// the Encryption object and carry the information it holds across subsystem boundaries. In most cases you should use
// GetSingleton() to get a globally scoped copy of the Encryption object. However, for tests you should use this
// function and hopefully, some time in the future, we can get rid of the singleton entirely.
func New(logger hclog.Logger, cMap configs.StateEncryptionMap) Encryption {
	return &encryption{
		configs: cMap.Configs,
		mutex:   sync.Mutex{},
		logger:  logger,
	}
}

func (e *encryption) RemoteState() (encryptionflow.StateFlow, error) {
	return e.build(configs.StateEncryptionKeyBackend, nil)
}

func (e *encryption) StateFile() (encryptionflow.StateFlow, error) {
	return e.build(configs.StateEncryptionKeyStateFile, nil)
}

func (e *encryption) PlanFile() (encryptionflow.PlanFlow, error) {
	return e.build(configs.StateEncryptionKeyPlanFile, nil)
}

func (e *encryption) RemoteStateDatasource(configKey string, def *configs.StateEncryption) (encryptionflow.StateFlow, error) {
	return e.build(configKey, def)
}

// build builds the encryption and decryption fallback configuration. This function should be called inside a
// lock from e.mutex to avoid parallel changes while the build is happening.
func (e *encryption) build(key string, def *configs.StateEncryption) (encryptionflow.Flow, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	cfg, ok := e.configs[key]

	if !ok {
		return nil, fmt.Errorf("missing encryption configuration for %q", key)
	}

	if def != nil {
		defCopy := *def
		defCopy.Merge(cfg)

		cfg = &defCopy
	}

	// TODO save the result of encryptionflow.New
	// TODO return encryptionflow.New(key, cfg, e.logger), nil
	return nil, nil
}
