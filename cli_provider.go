package mux

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ModelDiscoveryConfig configures how a provider discovers available models dynamically.
type ModelDiscoveryConfig struct {
	// Command args appended to the provider binary for CLI-based discovery.
	// E.g., ["models"] runs "opencode models".
	Command []string `json:"command,omitempty"`

	// CachePath is a file path for cache-based discovery (~ is expanded to home).
	// E.g., "~/.codex/models_cache.json".
	CachePath string `json:"cache_path,omitempty"`

	// APIURL is an HTTP endpoint for API-based discovery.
	// E.g., "https://api.anthropic.com/v1/models?limit=100".
	APIURL string `json:"api_url,omitempty"`

	// APIKeyEnv is the environment variable name for API authentication.
	// The value is sent as "x-api-key" header.
	APIKeyEnv string `json:"api_key_env,omitempty"`

	// APIHeaders are additional headers sent with API requests.
	// E.g., {"anthropic-version": "2023-06-01"}.
	APIHeaders map[string]string `json:"api_headers,omitempty"`

	// Format specifies how to parse discovery output:
	//   "lines"          - one model ID per line; "provider/model" prefix is stripped for the label
	//   "full-lines"     - one model ID per line; full ID is also used as the label
	//   "dash"           - "id - Label (annotation)" per line; header/footer lines are skipped
	//   "codex-cache"    - Codex models_cache.json format (filters by visibility: "list")
	//   "anthropic-api"  - Anthropic /v1/models response with data[].{id, display_name}
	Format string `json:"format"`

	// Filter excludes discovered models whose ID contains any of these substrings.
	Filter []string `json:"filter,omitempty"`

	// Aliases are static model entries prepended to discovery results.
	// These always appear regardless of discovery success.
	Aliases []ModelInfo `json:"aliases,omitempty"`
}

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
	// ModelDiscovery configures dynamic model discovery. When set, ListModels()
	// probes for available models instead of returning the static Models list.
	ModelDiscovery *ModelDiscoveryConfig `json:"model_discovery,omitempty"`
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
	disc := p.config.ModelDiscovery
	if disc == nil {
		return staticModels(p.config.Models), nil
	}

	var discovered []ModelInfo
	var err error

	switch {
	case disc.CachePath != "":
		discovered, err = discoverModelsFromCache(disc)
	case len(disc.Command) > 0:
		discovered, err = discoverModelsFromCLI(ctx, p.config.Binary, disc)
	case disc.APIURL != "":
		discovered, err = discoverModelsFromAPI(ctx, disc)
	}

	// Prepend aliases
	var result []ModelInfo
	result = append(result, disc.Aliases...)
	if len(discovered) > 0 {
		result = append(result, discovered...)
	} else if err != nil || len(discovered) == 0 {
		// Fall back to static models if discovery failed
		result = append(result, staticModels(p.config.Models)...)
	}

	return result, nil
}

// staticModels converts a []string model list to []ModelInfo.
func staticModels(ids []string) []ModelInfo {
	models := make([]ModelInfo, len(ids))
	for i, id := range ids {
		models[i] = ModelInfo{ID: id, Name: id}
	}
	return models
}

// discoverModelsFromCache reads a JSON cache file to discover models.
func discoverModelsFromCache(disc *ModelDiscoveryConfig) ([]ModelInfo, error) {
	path := disc.CachePath
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	switch disc.Format {
	case "codex-cache":
		return parseCodexCache(data, disc.Filter)
	default:
		return nil, fmt.Errorf("unknown cache format: %s", disc.Format)
	}
}

// discoverModelsFromCLI runs a CLI subcommand to discover models.
func discoverModelsFromCLI(ctx context.Context, binary string, disc *ModelDiscoveryConfig) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, binary, disc.Command...).Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	switch disc.Format {
	case "lines":
		return parseLinesFormat(lines, disc.Filter), nil
	case "full-lines":
		return parseFullLinesFormat(lines, disc.Filter), nil
	case "dash":
		return parseDashFormat(lines, disc.Filter), nil
	default:
		return parseLinesFormat(lines, disc.Filter), nil
	}
}

// discoverModelsFromAPI calls an HTTP endpoint to discover models.
func discoverModelsFromAPI(ctx context.Context, disc *ModelDiscoveryConfig) ([]ModelInfo, error) {
	apiKey := os.Getenv(disc.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("env var %s not set", disc.APIKeyEnv)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", disc.APIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	for k, v := range disc.APIHeaders {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	switch disc.Format {
	case "anthropic-api":
		return parseAnthropicAPI(body, disc.Filter)
	default:
		return nil, fmt.Errorf("unknown API format: %s", disc.Format)
	}
}

// parseCodexCache parses the Codex models_cache.json format.
func parseCodexCache(data []byte, filter []string) ([]ModelInfo, error) {
	var cache struct {
		Models []struct {
			Slug                     string `json:"slug"`
			DisplayName              string `json:"display_name"`
			Description              string `json:"description"`
			Visibility               string `json:"visibility"`
			SupportedReasoningLevels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range cache.Models {
		if m.Visibility != "list" {
			continue
		}
		if matchesFilter(m.Slug, filter) {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.Slug
		}
		mi := ModelInfo{ID: m.Slug, Name: name, Description: m.Description}
		for _, rl := range m.SupportedReasoningLevels {
			mi.Efforts = append(mi.Efforts, rl.Effort)
		}
		models = append(models, mi)
	}
	return models, nil
}

// parseAnthropicAPI parses the Anthropic /v1/models response format.
func parseAnthropicAPI(data []byte, filter []string) ([]ModelInfo, error) {
	var result struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	var models []ModelInfo
	for _, m := range result.Data {
		if matchesFilter(m.ID, filter) {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		models = append(models, ModelInfo{ID: m.ID, Name: name})
	}
	return models, nil
}

// parseLinesFormat parses one-model-per-line output (e.g., "provider/model").
func parseLinesFormat(lines []string, filter []string) []ModelInfo {
	var models []ModelInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if matchesFilter(line, filter) {
			continue
		}
		label := line
		if parts := strings.SplitN(line, "/", 2); len(parts) == 2 {
			label = parts[1]
		}
		models = append(models, ModelInfo{ID: line, Name: label})
	}
	return models
}

// parseFullLinesFormat parses one-model-per-line output and keeps IDs as labels.
func parseFullLinesFormat(lines []string, filter []string) []ModelInfo {
	var models []ModelInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if matchesFilter(line, filter) {
			continue
		}
		models = append(models, ModelInfo{ID: line, Name: line})
	}
	return models
}

// parseDashFormat parses "id - Label (annotation)" format lines.
func parseDashFormat(lines []string, filter []string) []ModelInfo {
	var models []ModelInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Available") || strings.HasPrefix(line, "Tip:") || strings.HasPrefix(line, "Loading") {
			continue
		}
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		id := strings.TrimSpace(parts[0])
		label := strings.TrimSpace(parts[1])
		if idx := strings.Index(label, "("); idx > 0 {
			label = strings.TrimSpace(label[:idx])
		}
		if matchesFilter(id, filter) {
			continue
		}
		models = append(models, ModelInfo{ID: id, Name: label})
	}
	return models
}

// matchesFilter returns true if s contains any of the filter substrings.
func matchesFilter(s string, filter []string) bool {
	for _, f := range filter {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
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
			Models:         []string{"sonnet", "opus", "haiku"},
			Efforts:        []string{"low", "medium", "high"},
			ModelDiscovery: &ModelDiscoveryConfig{
				APIURL:    "https://api.anthropic.com/v1/models?limit=100",
				APIKeyEnv: "ANTHROPIC_API_KEY",
				APIHeaders: map[string]string{
					"anthropic-version": "2023-06-01",
				},
				Format: "anthropic-api",
				Aliases: []ModelInfo{
					{ID: "sonnet", Name: "Sonnet (latest)"},
					{ID: "opus", Name: "Opus (latest)"},
					{ID: "haiku", Name: "Haiku (latest)"},
				},
			},
		},
		{
			ProviderID:     "codex",
			Name:           "Codex",
			Binary:         "codex",
			BaseArgs:       []string{"exec", "--json", "--dangerously-bypass-approvals-and-sandbox"},
			ParserType:     "codex",
			SupportsResume: false,
			ModelFlag:      "-m",
			DefaultModelID: "gpt-5.4",
			AttachmentMode: "prompt",
			Models:         []string{"gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.2"},
			ModelDiscovery: &ModelDiscoveryConfig{
				CachePath: "~/.codex/models_cache.json",
				Format:    "codex-cache",
			},
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
			ModelDiscovery: &ModelDiscoveryConfig{
				Command: []string{"models"},
				Format:  "full-lines",
				Filter:  []string{"claude"},
			},
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
			ModelDiscovery: &ModelDiscoveryConfig{
				Command: []string{"models"},
				Format:  "dash",
			},
		},
	}
}
