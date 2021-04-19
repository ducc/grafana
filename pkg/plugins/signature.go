package plugins

import (
	"fmt"

	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/setting"
)

var logger = log.New("plugin.signature.validator")

const (
	SignatureMissing  ErrorCode = "signatureMissing"
	SignatureModified ErrorCode = "signatureModified"
	SignatureInvalid  ErrorCode = "signatureInvalid"
)

type PluginSignatureValidator struct {
	cfg                           *setting.Cfg
	requireSigned                 bool
	errors                        []error
	allowUnsignedPluginsCondition UnsignedPluginV2ConditionFunc
}

func NewSignatureValidator(cfg *setting.Cfg, requireSigned bool, unsignedCond UnsignedPluginV2ConditionFunc) PluginSignatureValidator {
	return PluginSignatureValidator{
		cfg:                           cfg,
		requireSigned:                 requireSigned,
		allowUnsignedPluginsCondition: unsignedCond,
	}
}

type UnsignedPluginV2ConditionFunc = func(plugin *PluginV2) bool

func (s *PluginSignatureValidator) Validate(plugin *PluginV2) *PluginError {
	if plugin.Signature == PluginSignatureValid {
		logger.Debug("Plugin has valid signature", "id", plugin.ID)
		return nil
	}

	if plugin.Parent != nil {
		// If a descendant plugin with invalid signature, set signature to that of root
		if plugin.IsCorePlugin || plugin.Signature == PluginSignatureInternal {
			logger.Debug("Not setting descendant plugin's signature to that of root since it's core or internal",
				"plugin", plugin.ID, "signature", plugin.Signature, "isCore", plugin.IsCorePlugin)
		} else {
			logger.Debug("Setting descendant plugin's signature to that of root", "plugin", plugin.ID,
				"root", plugin.Parent.ID, "signature", plugin.Signature, "rootSignature", plugin.Parent.Signature)
			plugin.Signature = plugin.Parent.Signature
			if plugin.Signature == PluginSignatureValid {
				logger.Debug("Plugin has valid signature (inherited from root)", "id", plugin.ID)
				return nil
			}
		}
	} else {
		logger.Debug("Non-valid plugin Signature", "pluginID", plugin.ID, "pluginDir", plugin.PluginDir,
			"state", plugin.Signature)
	}

	// For the time being, we choose to only require back-end plugins to be signed
	// NOTE: the state is calculated again when setting metadata on the object
	if !plugin.Backend || !s.requireSigned {
		return nil
	}

	switch plugin.Signature {
	case PluginSignatureUnsigned:
		if allowed := s.allowUnsigned(plugin); !allowed {
			logger.Debug("Plugin is unsigned", "id", plugin.ID)
			s.errors = append(s.errors, fmt.Errorf("plugin %q is unsigned", plugin.ID))
			return &PluginError{
				ErrorCode: SignatureMissing,
			}
		}
		logger.Warn("Running an unsigned backend plugin", "pluginID", plugin.ID, "pluginDir",
			plugin.PluginDir)
		return nil
	case PluginSignatureInvalid:
		logger.Debug("Plugin %q has an invalid signature", plugin.ID)
		s.errors = append(s.errors, fmt.Errorf("plugin %q has an invalid signature", plugin.ID))
		return &PluginError{
			ErrorCode: SignatureInvalid,
		}
	case PluginSignatureModified:
		logger.Debug("Plugin %q has a modified signature", plugin.ID)
		s.errors = append(s.errors, fmt.Errorf("plugin %q's signature has been modified", plugin.ID))
		return &PluginError{
			ErrorCode: SignatureModified,
		}
	default:
		panic(fmt.Sprintf("Plugin %q has unrecognized plugin signature state %q", plugin.ID, plugin.Signature))
	}
}

func (s *PluginSignatureValidator) allowUnsigned(plugin *PluginV2) bool {
	if s.allowUnsignedPluginsCondition != nil {
		return s.allowUnsignedPluginsCondition(plugin)
	}

	if s.cfg.Env == setting.Dev {
		return true
	}

	for _, plug := range s.cfg.PluginsAllowUnsigned {
		if plug == plugin.ID {
			return true
		}
	}

	return false
}