package mux

import (
	"context"
	"os/exec"
)

type RuntimeConfigService struct {
	Store ProviderFileStore
}

func CLIProviderConfigFromProviderFile(file ProviderFileConfig) CLIProviderConfig {
	return CLIProviderConfig{ProviderID: file.ID, Name: file.Name, Binary: file.Binary, BaseArgs: file.Execution.BaseArgs, ParserType: file.ParserType, SupportsResume: file.Execution.ResumeFlag != "", ResumeFlag: file.Execution.ResumeFlag, ModelFlag: file.Execution.ModelFlag, EffortFlag: file.Execution.EffortFlag, SubAgentFlag: file.Execution.SubAgentFlag, MCPMode: file.Execution.MCPMode, EnvVars: file.Execution.EnvVars, DefaultModelID: file.Execution.DefaultModelID, AttachmentMode: file.Execution.AttachmentMode, AttachmentFlag: file.Execution.AttachmentFlag, Models: file.Models, Efforts: file.Efforts, SubAgents: file.SubAgents, ModelDiscovery: file.Discovery}
}

func NewRuntimeConfigService(store ProviderFileStore) RuntimeConfigService {
	return RuntimeConfigService{Store: store}
}

func (s RuntimeConfigService) List(ctx context.Context) ([]ProviderRuntimeConfig, []ConfigValidationError, error) {
	files, errs, err := s.Store.Load(ctx)
	if err != nil {
		return nil, errs, err
	}
	runtime := make([]ProviderRuntimeConfig, 0, len(files))
	for _, file := range files {
		status := ProviderStatusReady
		warnings := []string{}
		if len(file.Validate()) > 0 {
			status = ProviderStatusInvalidConfig
		}
		if _, err := exec.LookPath(file.Binary); err != nil {
			status = ProviderStatusMissingBinary
			warnings = append(warnings, "provider binary is not available on PATH")
		}
		runtime = append(runtime, ProviderRuntimeConfig{ID: file.ID, Name: file.Name, Binary: file.Binary, ParserType: file.ParserType, Enabled: file.Enabled, Status: status, Warnings: warnings, Sections: file.Sections, Execution: file.Execution})
	}
	return runtime, errs, nil
}
