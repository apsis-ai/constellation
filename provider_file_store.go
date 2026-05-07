package mux

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type ProviderFileStore struct {
	Dir string
}

func DefaultProviderConfigDir() string {
	if v := os.Getenv("CONSTELLATION_PROVIDER_CONFIG_DIR"); v != "" {
		return v
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		return filepath.Join(cfg, "constellation", "providers")
	}
	return filepath.Join(os.TempDir(), "constellation", "providers")
}

func NewProviderFileStore(dir string) ProviderFileStore {
	if dir == "" {
		dir = DefaultProviderConfigDir()
	}
	return ProviderFileStore{Dir: dir}
}

func (s ProviderFileStore) SeedBuiltins(ctx context.Context, configs []CLIProviderConfig) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	for _, cfg := range configs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		path := filepath.Join(s.Dir, cfg.ProviderID+".json")
		fileCfg := ProviderFileConfigFromCLI(cfg)
		if data, err := os.ReadFile(path); err == nil {
			var existing ProviderFileConfig
			if err := json.Unmarshal(data, &existing); err != nil {
				return err
			}
			fileCfg = mergeProviderFileBuiltin(existing, fileCfg)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		data, err := json.MarshalIndent(fileCfg, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (s ProviderFileStore) Load(ctx context.Context) ([]ProviderFileConfig, []ConfigValidationError, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var configs []ProviderFileConfig
	var allErrs []ConfigValidationError
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, allErrs, ctx.Err()
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, entry.Name()))
		if err != nil {
			return nil, allErrs, err
		}
		var cfg ProviderFileConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			allErrs = append(allErrs, ConfigValidationError{Path: entry.Name(), Code: "invalid_json", Message: err.Error()})
			continue
		}
		if errs := cfg.Validate(); len(errs) > 0 {
			allErrs = append(allErrs, errs...)
		}
		configs = append(configs, cfg)
	}
	return configs, allErrs, nil
}

func mergeProviderFileBuiltin(existing, builtin ProviderFileConfig) ProviderFileConfig {
	merged := existing
	if merged.ID == "" {
		merged.ID = builtin.ID
	}
	if merged.Name == "" {
		merged.Name = builtin.Name
	}
	if merged.Binary == "" {
		merged.Binary = builtin.Binary
	}
	if merged.ParserType == "" {
		merged.ParserType = builtin.ParserType
	}
	if len(merged.Execution.BaseArgs) == 0 {
		merged.Execution.BaseArgs = builtin.Execution.BaseArgs
	}
	if merged.Execution.ResumeFlag == "" {
		merged.Execution.ResumeFlag = builtin.Execution.ResumeFlag
	}
	if merged.Execution.ModelFlag == "" {
		merged.Execution.ModelFlag = builtin.Execution.ModelFlag
	}
	if merged.Execution.EffortFlag == "" || merged.ID == "codex" {
		merged.Execution.EffortFlag = builtin.Execution.EffortFlag
	}
	if merged.Execution.SubAgentFlag == "" {
		merged.Execution.SubAgentFlag = builtin.Execution.SubAgentFlag
	}
	if merged.Execution.MCPMode == "" {
		merged.Execution.MCPMode = builtin.Execution.MCPMode
	}
	if merged.Execution.PromptSeparator == nil {
		merged.Execution.PromptSeparator = builtin.Execution.PromptSeparator
	}
	if merged.Execution.DefaultModelID == "" {
		merged.Execution.DefaultModelID = builtin.Execution.DefaultModelID
	}
	if merged.Execution.AttachmentMode == "" {
		merged.Execution.AttachmentMode = builtin.Execution.AttachmentMode
	}
	if merged.Execution.AttachmentFlag == "" {
		merged.Execution.AttachmentFlag = builtin.Execution.AttachmentFlag
	}
	if len(merged.Execution.EnvVars) == 0 {
		merged.Execution.EnvVars = builtin.Execution.EnvVars
	}
	if len(merged.Models) == 0 {
		merged.Models = builtin.Models
	}
	if len(merged.Efforts) == 0 {
		merged.Efforts = builtin.Efforts
	}
	if len(merged.SubAgents) == 0 {
		merged.SubAgents = builtin.SubAgents
	}
	if merged.Discovery == nil {
		merged.Discovery = builtin.Discovery
	}

	sectionIDs := make(map[string]bool, len(merged.Sections))
	fieldKeys := make(map[string]bool)
	builtinFields := map[string]ConfigField{}
	for _, section := range builtin.Sections {
		for _, field := range section.Fields {
			builtinFields[field.Key] = field
		}
	}
	for si, section := range merged.Sections {
		sectionIDs[section.ID] = true
		for fi, field := range section.Fields {
			fieldKeys[field.Key] = true
			if builtinField, ok := builtinFields[field.Key]; ok {
				merged.Sections[si].Fields[fi] = mergeProviderFieldBuiltin(field, builtinField)
			}
		}
	}
	for _, section := range builtin.Sections {
		if sectionIDs[section.ID] {
			continue
		}
		missingField := false
		for _, field := range section.Fields {
			if !fieldKeys[field.Key] {
				missingField = true
				break
			}
		}
		if missingField {
			merged.Sections = append(merged.Sections, section)
		}
	}

	return merged
}

func mergeProviderFieldBuiltin(existing, builtin ConfigField) ConfigField {
	merged := existing
	if merged.Type == "" {
		merged.Type = builtin.Type
	}
	if merged.Label == "" {
		merged.Label = builtin.Label
	}
	if len(merged.Options) == 0 {
		merged.Options = builtin.Options
	}
	if merged.OptionsSource == nil {
		merged.OptionsSource = builtin.OptionsSource
	}
	if len(merged.FallbackOptions) == 0 {
		merged.FallbackOptions = builtin.FallbackOptions
	}
	if builtin.Mapping != nil {
		merged.Mapping = builtin.Mapping
	}
	return merged
}

func ProviderFileConfigFromCLI(cfg CLIProviderConfig) ProviderFileConfig {
	sections := []ConfigSection{}
	if len(cfg.SubAgents) > 0 || cfg.SubAgentFlag != "" {
		sections = append(sections, ConfigSection{ID: "agent", Label: "Agent", Fields: []ConfigField{{Key: "agent_sub", Type: FieldTypeSelect, Label: "Agent", Default: "default", Options: configOptionsFromStrings(cfg.SubAgents), Mapping: &ExecutionMapping{Kind: "arg", Name: cfg.SubAgentFlag}}}})
	}
	sections = append(sections, ConfigSection{ID: "model", Label: "Model", Fields: []ConfigField{{Key: "model", Type: FieldTypeSelect, Label: "Model", Required: cfg.ModelFlag != "", Default: cfg.DefaultModelID, Options: configOptionsFromStrings(cfg.Models), OptionsSource: modelOptionsSource(cfg), Mapping: &ExecutionMapping{Kind: "arg", Name: cfg.ModelFlag}}}})
	if len(cfg.Efforts) > 0 || cfg.EffortFlag != "" {
		sections = append(sections, ConfigSection{ID: "effort", Label: "Effort", Fields: []ConfigField{{Key: "effort", Type: FieldTypeSelect, Label: "Effort", Options: configOptionsFromStrings(cfg.Efforts), Mapping: effortMapping(cfg)}}})
	}
	return ProviderFileConfig{ID: cfg.ProviderID, Name: cfg.Name, Binary: cfg.Binary, ParserType: cfg.ParserType, Enabled: true, Execution: ProviderExecutionConfig{BaseArgs: cfg.BaseArgs, ResumeFlag: cfg.ResumeFlag, ModelFlag: cfg.ModelFlag, EffortFlag: cfg.EffortFlag, SubAgentFlag: cfg.SubAgentFlag, MCPMode: cfg.MCPMode, PromptSeparator: cfg.PromptSeparator, DefaultModelID: cfg.DefaultModelID, AttachmentMode: cfg.AttachmentMode, AttachmentFlag: cfg.AttachmentFlag, EnvVars: cfg.EnvVars}, Sections: sections, Models: cfg.Models, Efforts: cfg.Efforts, SubAgents: cfg.SubAgents, Discovery: cfg.ModelDiscovery}
}

func effortMapping(cfg CLIProviderConfig) *ExecutionMapping {
	if cfg.ProviderID == "codex" {
		return &ExecutionMapping{Kind: "arg", Name: "-c", Mode: "model_reasoning_effort"}
	}
	return &ExecutionMapping{Kind: "arg", Name: cfg.EffortFlag}
}

func modelOptionsSource(cfg CLIProviderConfig) *OptionsSource {
	if cfg.ModelDiscovery == nil {
		return nil
	}
	return &OptionsSource{Type: OptionSourceModelDiscovery, Command: cfg.ModelDiscovery.Command, Format: cfg.ModelDiscovery.Format}
}

func configOptionsFromStrings(values []string) []ConfigOption {
	out := make([]ConfigOption, 0, len(values))
	for _, v := range values {
		out = append(out, ConfigOption{Value: v, Label: v})
	}
	return out
}
