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
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fileCfg := ProviderFileConfigFromCLI(cfg)
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

func ProviderFileConfigFromCLI(cfg CLIProviderConfig) ProviderFileConfig {
	sections := []ConfigSection{}
	if len(cfg.SubAgents) > 0 || cfg.SubAgentFlag != "" {
		sections = append(sections, ConfigSection{ID: "agent", Label: "Agent", Fields: []ConfigField{{Key: "agent_sub", Type: FieldTypeSelect, Label: "Agent", Default: "default", Options: configOptionsFromStrings(cfg.SubAgents), Mapping: &ExecutionMapping{Kind: "arg", Name: cfg.SubAgentFlag}}}})
	}
	sections = append(sections, ConfigSection{ID: "model", Label: "Model", Fields: []ConfigField{{Key: "model", Type: FieldTypeSelect, Label: "Model", Required: cfg.ModelFlag != "", Default: cfg.DefaultModelID, Options: configOptionsFromStrings(cfg.Models), OptionsSource: modelOptionsSource(cfg), Mapping: &ExecutionMapping{Kind: "arg", Name: cfg.ModelFlag}}}})
	if len(cfg.Efforts) > 0 || cfg.EffortFlag != "" {
		sections = append(sections, ConfigSection{ID: "effort", Label: "Effort", Fields: []ConfigField{{Key: "effort", Type: FieldTypeSelect, Label: "Effort", Options: configOptionsFromStrings(cfg.Efforts), Mapping: &ExecutionMapping{Kind: "arg", Name: cfg.EffortFlag}}}})
	}
	return ProviderFileConfig{ID: cfg.ProviderID, Name: cfg.Name, Binary: cfg.Binary, ParserType: cfg.ParserType, Enabled: true, Execution: ProviderExecutionConfig{BaseArgs: cfg.BaseArgs, ResumeFlag: cfg.ResumeFlag, ModelFlag: cfg.ModelFlag, EffortFlag: cfg.EffortFlag, SubAgentFlag: cfg.SubAgentFlag, MCPMode: cfg.MCPMode, DefaultModelID: cfg.DefaultModelID, AttachmentMode: cfg.AttachmentMode, AttachmentFlag: cfg.AttachmentFlag, EnvVars: cfg.EnvVars}, Sections: sections, Models: cfg.Models, Efforts: cfg.Efforts, SubAgents: cfg.SubAgents, Discovery: cfg.ModelDiscovery}
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
