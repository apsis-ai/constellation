package mux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// CLIProviderConfig holds configuration for a CLI-based provider.
type CLIProviderConfig struct {
	ProviderID     string            `json:"id"`
	Name           string            `json:"name"`
	Binary         string            `json:"binary"`
	BaseArgs       []string          `json:"base_args"`
	ParserType     string            `json:"parser_type"`
	SupportsResume bool              `json:"supports_resume"`
	ResumeFlag     string            `json:"resume_flag,omitempty"`
	ModelFlag      string            `json:"model_flag,omitempty"`
	EffortFlag     string            `json:"effort_flag,omitempty"`
	SubAgentFlag   string            `json:"subagent_flag,omitempty"`
	MCPMode        string            `json:"mcp_mode,omitempty"` // "flag", "workspace", or ""
	EnvVars        map[string]string `json:"env_vars,omitempty"`
	DefaultModelID string            `json:"default_model,omitempty"`
	// AttachmentMode: "prompt" (prefix to prompt) or "flag" (--file per attachment)
	AttachmentMode string   `json:"attachment_mode,omitempty"`
	AttachmentFlag string   `json:"attachment_flag,omitempty"`
	Models         []string `json:"models,omitempty"`
	Efforts        []string `json:"efforts,omitempty"`
	SubAgents      []string `json:"sub_agents,omitempty"`
}

// CLIProvider executes prompts via CLI subprocess.
type CLIProvider struct {
	config  CLIProviderConfig
	parsers *ParserRegistry
}

// NewCLIProvider creates a CLIProvider with the given config and parser registry.
func NewCLIProvider(cfg CLIProviderConfig, parsers *ParserRegistry) *CLIProvider {
	return &CLIProvider{config: cfg, parsers: parsers}
}

func (p *CLIProvider) ID() string { return p.config.ProviderID }

func (p *CLIProvider) SupportsResume() bool { return p.config.SupportsResume }

func (p *CLIProvider) DefaultModel() string { return p.config.DefaultModelID }

func (p *CLIProvider) Validate() error {
	_, err := exec.LookPath(p.config.Binary)
	if err != nil {
		return fmt.Errorf("%s not found on PATH: %w", p.config.Binary, err)
	}
	return nil
}

func (p *CLIProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return nil, nil // Not yet implemented for CLI providers
}

// Execute is implemented in Phase 3 when Manager integration happens.
// For now, it returns an error indicating it should be called through Manager.
func (p *CLIProvider) Execute(ctx context.Context, req ProviderRequest, ch chan<- ChanEvent) (ProviderResult, error) {
	return ProviderResult{}, fmt.Errorf("CLIProvider.Execute not yet wired — use Manager.Send()")
}

// BuildArgs constructs the CLI arguments for the given request.
// This is the core logic extracted from buildXxxCommand functions.
func (p *CLIProvider) BuildArgs(req ProviderRequest) []string {
	args := make([]string, len(p.config.BaseArgs))
	copy(args, p.config.BaseArgs)

	if req.MCPConfig != "" && p.config.MCPMode == "flag" {
		mcpDir := filepath.Join(os.TempDir(), "agents-mux-mcp")
		os.MkdirAll(mcpDir, 0755)
		mcpPath := filepath.Join(mcpDir, fmt.Sprintf("mcp-%s.json", req.SessionID))
		os.WriteFile(mcpPath, []byte(req.MCPConfig), 0644)
		args = append(args, "--mcp-config", mcpPath)
	}

	if req.Model != "" && p.config.ModelFlag != "" {
		args = append(args, p.config.ModelFlag, req.Model)
	}
	if req.Effort != "" && p.config.EffortFlag != "" {
		args = append(args, p.config.EffortFlag, req.Effort)
	}
	if req.SubAgent != "" && req.SubAgent != "default" && p.config.SubAgentFlag != "" {
		args = append(args, p.config.SubAgentFlag, req.SubAgent)
	}
	if req.ConversationID != "" && p.config.ResumeFlag != "" {
		args = append(args, p.config.ResumeFlag, req.ConversationID)
	}

	// Handle attachments
	prompt := req.Prompt
	if p.config.AttachmentMode == "flag" && p.config.AttachmentFlag != "" {
		for _, att := range req.Attachments {
			args = append(args, p.config.AttachmentFlag, att.Path)
		}
	} else {
		prompt = buildAttachmentPrompt(prompt, req.Attachments)
	}

	args = append(args, "--", prompt)
	return args
}

// BuildCommand constructs an exec.Cmd for the given request.
func (p *CLIProvider) BuildCommand(req ProviderRequest) (*exec.Cmd, error) {
	binPath, err := exec.LookPath(p.config.Binary)
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH: %w", p.config.Binary, err)
	}

	args := p.BuildArgs(req)

	// For workspace-based providers (Cursor), dynamically add --workspace and set up MCP
	if p.config.MCPMode == "workspace" {
		cursorWorkspace := filepath.Join(os.TempDir(), "agents-mux-cursor")
		args = append([]string{args[0]}, append(args[1:len(args)-2], "--workspace", cursorWorkspace, args[len(args)-2], args[len(args)-1])...)
		cursorDir := filepath.Join(cursorWorkspace, ".cursor")
		os.MkdirAll(cursorDir, 0755)
		if req.MCPConfig != "" {
			os.WriteFile(filepath.Join(cursorDir, "mcp.json"), []byte(req.MCPConfig), 0644)
		}
	}

	cmd := exec.Command(binPath, args...)
	if p.config.MCPMode == "workspace" {
		cmd.Dir = filepath.Join(os.TempDir(), "agents-mux-cursor")
	}

	// Apply environment variables
	if len(req.Env) > 0 {
		cmd.Env = req.Env
	} else {
		cmd.Env = os.Environ()
	}

	return cmd, nil
}

// GetParser returns the OutputParser for this provider.
func (p *CLIProvider) GetParser(cb ParserCallbacks) (OutputParser, error) {
	factory, ok := p.parsers.GetParser(p.config.ParserType)
	if !ok {
		return nil, fmt.Errorf("unknown parser type: %s", p.config.ParserType)
	}
	return factory(cb), nil
}

// BuiltinCLIConfigs returns the default configs for built-in providers.
func BuiltinCLIConfigs() []CLIProviderConfig {
	return []CLIProviderConfig{
		{
			ProviderID:     "claude",
			Name:           "Claude",
			Binary:         "claude",
			BaseArgs:       []string{"-p", "--output-format", "stream-json", "--verbose", "--dangerously-skip-permissions"},
			ParserType:     "claude",
			SupportsResume: true,
			ResumeFlag:     "--resume",
			ModelFlag:      "--model",
			EffortFlag:     "--effort",
			SubAgentFlag:   "--agent",
			MCPMode:        "flag",
			DefaultModelID: "sonnet",
			AttachmentMode: "prompt",
			Models:         []string{"sonnet", "opus"},
			Efforts:        []string{"low", "medium", "high"},
		},
		{
			ProviderID:     "codex",
			Name:           "Codex",
			Binary:         "codex",
			BaseArgs:       []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox"},
			ParserType:     "codex",
			SupportsResume: false,
			ModelFlag:      "-m",
			DefaultModelID: "o4-mini",
			AttachmentMode: "prompt",
			Models:         []string{"o3", "codex-mini-latest"},
		},
		{
			ProviderID:     "opencode",
			Name:           "OpenCode",
			Binary:         "opencode",
			BaseArgs:       []string{"run", "--format", "json"},
			ParserType:     "opencode",
			SupportsResume: true,
			ResumeFlag:     "-s",
			ModelFlag:      "-m",
			EffortFlag:     "--variant",
			SubAgentFlag:   "--agent",
			AttachmentMode: "flag",
			AttachmentFlag: "--file",
			Efforts:        []string{"minimal", "high", "max"},
		},
		{
			ProviderID:     "agent",
			Name:           "Agent",
			Binary:         "agent",
			BaseArgs:       []string{"-p", "--output-format", "stream-json", "--stream-partial-output", "--force", "--approve-mcps", "--trust"},
			ParserType:     "cursor",
			SupportsResume: true,
			ResumeFlag:     "--resume",
			ModelFlag:      "--model",
			MCPMode:        "workspace",
			AttachmentMode: "prompt",
		},
	}
}
