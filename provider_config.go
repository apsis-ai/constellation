package mux

import "fmt"

type ProviderStatus string

const (
	ProviderStatusReady          ProviderStatus = "ready"
	ProviderStatusWarning        ProviderStatus = "warning"
	ProviderStatusMissingBinary  ProviderStatus = "missing_binary"
	ProviderStatusInvalidConfig  ProviderStatus = "invalid_config"
	ProviderStatusDiscoveryError ProviderStatus = "discovery_error"
)

type FieldType string

const (
	FieldTypeSelect      FieldType = "select"
	FieldTypeMultiSelect FieldType = "multiselect"
	FieldTypeText        FieldType = "text"
	FieldTypeNumber      FieldType = "number"
	FieldTypeBoolean     FieldType = "boolean"
	FieldTypePath        FieldType = "path"
)

type OptionSourceType string

const (
	OptionSourceStatic            OptionSourceType = "static"
	OptionSourceModelDiscovery    OptionSourceType = "model_discovery"
	OptionSourceDerivedFromModels OptionSourceType = "derived_from_models"
	OptionSourceEnvStatus         OptionSourceType = "env_status"
)

type ConfigOption struct {
	Value    string         `json:"value"`
	Label    string         `json:"label"`
	Disabled bool           `json:"disabled,omitempty"`
	Meta     map[string]any `json:"meta,omitempty"`
}

type OptionsSource struct {
	Type      OptionSourceType `json:"type"`
	Command   []string         `json:"command,omitempty"`
	Format    string           `json:"format,omitempty"`
	Separator string           `json:"separator,omitempty"`
	Segment   int              `json:"segment,omitempty"`
	FilterBy  *FieldFilter     `json:"filter_by,omitempty"`
}

type FieldFilter struct {
	Field string `json:"field"`
	Path  string `json:"path"`
}

type ExecutionMapping struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Mode string `json:"mode,omitempty"`
}

type ConfigField struct {
	Key             string            `json:"key"`
	Type            FieldType         `json:"type"`
	Label           string            `json:"label"`
	Required        bool              `json:"required,omitempty"`
	Default         any               `json:"default,omitempty"`
	Options         []ConfigOption    `json:"options,omitempty"`
	OptionsSource   *OptionsSource    `json:"options_source,omitempty"`
	FallbackOptions []ConfigOption    `json:"fallback_options,omitempty"`
	Mapping         *ExecutionMapping `json:"mapping,omitempty"`
	Disabled        bool              `json:"disabled,omitempty"`
	Warning         string            `json:"warning,omitempty"`
}

type ConfigSection struct {
	ID     string        `json:"id"`
	Label  string        `json:"label"`
	Fields []ConfigField `json:"fields"`
}

type ProviderExecutionConfig struct {
	BaseArgs       []string          `json:"base_args,omitempty"`
	ResumeFlag     string            `json:"resume_flag,omitempty"`
	ModelFlag      string            `json:"model_flag,omitempty"`
	EffortFlag     string            `json:"effort_flag,omitempty"`
	SubAgentFlag   string            `json:"subagent_flag,omitempty"`
	MCPMode        string            `json:"mcp_mode,omitempty"`
	DefaultModelID string            `json:"default_model,omitempty"`
	AttachmentMode string            `json:"attachment_mode,omitempty"`
	AttachmentFlag string            `json:"attachment_flag,omitempty"`
	EnvVars        map[string]string `json:"env_vars,omitempty"`
}

type ProviderFileConfig struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Binary     string                  `json:"binary"`
	ParserType string                  `json:"parser_type"`
	Enabled    bool                    `json:"enabled"`
	Execution  ProviderExecutionConfig `json:"execution"`
	Sections   []ConfigSection         `json:"sections"`
	Models     []string                `json:"models,omitempty"`
	Efforts    []string                `json:"efforts,omitempty"`
	SubAgents  []string                `json:"sub_agents,omitempty"`
	Discovery  *ModelDiscoveryConfig   `json:"model_discovery,omitempty"`
}

type ProviderRuntimeConfig struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Binary     string                  `json:"binary"`
	ParserType string                  `json:"parser_type"`
	Enabled    bool                    `json:"enabled"`
	Status     ProviderStatus          `json:"status"`
	Warnings   []string                `json:"warnings,omitempty"`
	Sections   []ConfigSection         `json:"sections"`
	Execution  ProviderExecutionConfig `json:"-"`
}

type ConfigValidationError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c ProviderFileConfig) Validate() []ConfigValidationError {
	var errs []ConfigValidationError
	if c.ID == "" {
		errs = append(errs, ConfigValidationError{"id", "required", "provider id is required"})
	}
	if c.Name == "" {
		errs = append(errs, ConfigValidationError{"name", "required", "provider name is required"})
	}
	if c.Binary == "" {
		errs = append(errs, ConfigValidationError{"binary", "required", "provider binary is required"})
	}
	if c.ParserType == "" {
		errs = append(errs, ConfigValidationError{"parser_type", "required", "parser type is required"})
	}
	seen := map[string]bool{}
	for si, section := range c.Sections {
		if section.ID == "" {
			errs = append(errs, ConfigValidationError{fmt.Sprintf("sections.%d.id", si), "required", "section id is required"})
		}
		for fi, field := range section.Fields {
			path := fmt.Sprintf("sections.%d.fields.%d", si, fi)
			if field.Key == "" {
				errs = append(errs, ConfigValidationError{path + ".key", "required", "field key is required"})
			}
			if seen[field.Key] {
				errs = append(errs, ConfigValidationError{path + ".key", "duplicate", "field key must be unique"})
			}
			seen[field.Key] = true
			switch field.Type {
			case FieldTypeSelect, FieldTypeMultiSelect, FieldTypeText, FieldTypeNumber, FieldTypeBoolean, FieldTypePath:
			default:
				errs = append(errs, ConfigValidationError{path + ".type", "unsupported", "field type is not supported"})
			}
		}
	}
	return errs
}
