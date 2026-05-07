package mux

import (
	"context"
	"os/exec"
)

type RuntimeConfigService struct {
	Store    ProviderFileStore
	Resolver ConfigDiscoveryResolver
}

func CLIProviderConfigFromProviderFile(file ProviderFileConfig) CLIProviderConfig {
	promptSeparator := file.Execution.PromptSeparator
	if file.ID == "pi" && promptSeparator == nil {
		noPromptSeparator := ""
		promptSeparator = &noPromptSeparator
	}
	return CLIProviderConfig{ProviderID: file.ID, Name: file.Name, Binary: file.Binary, BaseArgs: file.Execution.BaseArgs, ParserType: file.ParserType, SupportsResume: file.Execution.ResumeFlag != "", ResumeFlag: file.Execution.ResumeFlag, ModelFlag: file.Execution.ModelFlag, EffortFlag: file.Execution.EffortFlag, SubAgentFlag: file.Execution.SubAgentFlag, MCPMode: file.Execution.MCPMode, EnvVars: file.Execution.EnvVars, PromptSeparator: promptSeparator, DefaultModelID: file.Execution.DefaultModelID, AttachmentMode: file.Execution.AttachmentMode, AttachmentFlag: file.Execution.AttachmentFlag, Models: file.Models, Efforts: file.Efforts, SubAgents: file.SubAgents, ModelDiscovery: file.Discovery}
}

func NewRuntimeConfigService(store ProviderFileStore) RuntimeConfigService {
	return RuntimeConfigService{Store: store, Resolver: NewConfigDiscoveryResolver("", 0)}
}

func (s RuntimeConfigService) List(ctx context.Context) ([]ProviderRuntimeConfig, []ConfigValidationError, error) {
	return s.list(ctx, "", false)
}

func (s RuntimeConfigService) Refresh(ctx context.Context, providerID string) ([]ProviderRuntimeConfig, []ConfigValidationError, error) {
	return s.list(ctx, providerID, true)
}

func (s RuntimeConfigService) list(ctx context.Context, providerID string, force bool) ([]ProviderRuntimeConfig, []ConfigValidationError, error) {
	files, errs, err := s.Store.Load(ctx)
	if err != nil {
		return nil, errs, err
	}
	runtime := make([]ProviderRuntimeConfig, 0, len(files))
	for _, file := range files {
		if providerID != "" && file.ID != providerID {
			continue
		}
		status := ProviderStatusReady
		warnings := []string{}
		if len(file.Validate()) > 0 {
			status = ProviderStatusInvalidConfig
		}
		if _, err := exec.LookPath(file.Binary); err != nil {
			status = ProviderStatusMissingBinary
			warnings = append(warnings, "provider binary is not available on PATH")
		}
		sections := s.resolveSections(ctx, file, force, &warnings)
		runtime = append(runtime, ProviderRuntimeConfig{ID: file.ID, Name: file.Name, Binary: file.Binary, ParserType: file.ParserType, Enabled: file.Enabled, Status: status, Warnings: warnings, Sections: sections, Execution: file.Execution})
	}
	return runtime, errs, nil
}

func (s RuntimeConfigService) resolveSections(ctx context.Context, file ProviderFileConfig, force bool, warnings *[]string) []ConfigSection {
	sections := make([]ConfigSection, len(file.Sections))
	modelOptions := []ConfigOption{}
	for si, section := range file.Sections {
		sections[si] = section
		sections[si].Fields = make([]ConfigField, len(section.Fields))
		for fi, field := range section.Fields {
			resolved := field
			options, warning := s.Resolver.Resolve(ctx, file, field, force)
			resolved.Options = options
			if field.Key == "model" {
				modelOptions = options
			}
			if warning != "" {
				resolved.Warning = warning
				*warnings = append(*warnings, field.Key+": "+warning)
			}
			sections[si].Fields[fi] = resolved
		}
	}
	applyDiscoveredEffortOptions(sections, modelOptions)
	return sections
}

func applyDiscoveredEffortOptions(sections []ConfigSection, modelOptions []ConfigOption) {
	efforts := effortOptionsFromModelOptions(modelOptions)
	if len(efforts) == 0 {
		return
	}
	for si := range sections {
		for fi := range sections[si].Fields {
			if sections[si].Fields[fi].Key == "effort" {
				sections[si].Fields[fi].Options = efforts
			}
		}
	}
}

func effortOptionsFromModelOptions(modelOptions []ConfigOption) []ConfigOption {
	seen := map[string]bool{}
	out := []ConfigOption{}
	for _, model := range modelOptions {
		rawEfforts, ok := model.Meta["efforts"]
		if !ok {
			continue
		}
		for _, effort := range effortStrings(rawEfforts) {
			if effort == "" || seen[effort] {
				continue
			}
			seen[effort] = true
			out = append(out, ConfigOption{Value: effort, Label: effort})
		}
	}
	return out
}

func effortStrings(raw any) []string {
	switch efforts := raw.(type) {
	case []string:
		return efforts
	case []any:
		out := make([]string, 0, len(efforts))
		for _, effort := range efforts {
			if s, ok := effort.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
