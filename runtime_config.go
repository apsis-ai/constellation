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
	return CLIProviderConfig{ProviderID: file.ID, Name: file.Name, Binary: file.Binary, BaseArgs: file.Execution.BaseArgs, ParserType: file.ParserType, SupportsResume: file.Execution.ResumeFlag != "", ResumeFlag: file.Execution.ResumeFlag, ModelFlag: file.Execution.ModelFlag, EffortFlag: file.Execution.EffortFlag, SubAgentFlag: file.Execution.SubAgentFlag, MCPMode: file.Execution.MCPMode, EnvVars: file.Execution.EnvVars, DefaultModelID: file.Execution.DefaultModelID, AttachmentMode: file.Execution.AttachmentMode, AttachmentFlag: file.Execution.AttachmentFlag, Models: file.Models, Efforts: file.Efforts, SubAgents: file.SubAgents, ModelDiscovery: file.Discovery}
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
	for si, section := range file.Sections {
		sections[si] = section
		sections[si].Fields = make([]ConfigField, len(section.Fields))
		for fi, field := range section.Fields {
			resolved := field
			options, warning := s.Resolver.Resolve(ctx, file, field, force)
			resolved.Options = options
			if warning != "" {
				resolved.Warning = warning
				*warnings = append(*warnings, field.Key+": "+warning)
			}
			sections[si].Fields[fi] = resolved
		}
	}
	return sections
}
