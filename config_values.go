package mux

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type ConfigValues map[string]any

func MarshalConfigValues(values map[string]any) string {
	if values == nil {
		return "{}"
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func UnmarshalConfigValues(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var values map[string]any
	if json.Unmarshal([]byte(raw), &values) != nil {
		return map[string]any{}
	}
	if values == nil {
		return map[string]any{}
	}
	return values
}

func ValidateConfigValues(provider ProviderRuntimeConfig, values ConfigValues) []ConfigValidationError {
	var errs []ConfigValidationError
	fields := map[string]ConfigField{}
	for _, section := range provider.Sections {
		for _, field := range section.Fields {
			fields[field.Key] = field
		}
	}
	for key := range values {
		if _, ok := fields[key]; !ok {
			errs = append(errs, ConfigValidationError{Path: "config_values." + key, Code: "unknown_field", Message: "field is not declared by provider config"})
		}
	}
	for key, field := range fields {
		value, ok := values[key]
		if (!ok || value == "") && field.Default != nil {
			value = field.Default
			ok = true
		}
		if field.Required && (!ok || value == "") {
			errs = append(errs, ConfigValidationError{Path: "config_values." + key, Code: "required", Message: "required field is missing"})
			continue
		}
		if !ok || value == "" {
			continue
		}
		switch field.Type {
		case FieldTypeSelect:
			selected, ok := value.(string)
			if !ok {
				errs = append(errs, ConfigValidationError{Path: "config_values." + key, Code: "invalid_type", Message: "expected string"})
				continue
			}
			if len(field.Options) > 0 && !optionExists(field.Options, selected) {
				errs = append(errs, ConfigValidationError{Path: "config_values." + key, Code: "invalid_option", Message: fmt.Sprintf("%s is not an allowed option", selected)})
			}
		case FieldTypeMultiSelect:
			items, ok := stringSlice(value)
			if !ok {
				errs = append(errs, ConfigValidationError{Path: "config_values." + key, Code: "invalid_type", Message: "expected string array"})
				continue
			}
			for _, item := range items {
				if len(field.Options) > 0 && !optionExists(field.Options, item) {
					errs = append(errs, ConfigValidationError{Path: "config_values." + key, Code: "invalid_option", Message: fmt.Sprintf("%s is not an allowed option", item)})
				}
			}
		case FieldTypeBoolean:
			if _, ok := value.(bool); !ok {
				errs = append(errs, ConfigValidationError{Path: "config_values." + key, Code: "invalid_type", Message: "expected boolean"})
			}
		case FieldTypeNumber:
			switch value.(type) {
			case float64, int, int64:
			default:
				errs = append(errs, ConfigValidationError{Path: "config_values." + key, Code: "invalid_type", Message: "expected number"})
			}
		case FieldTypeText, FieldTypePath:
			if _, ok := value.(string); !ok {
				errs = append(errs, ConfigValidationError{Path: "config_values." + key, Code: "invalid_type", Message: "expected string"})
			}
		}
	}
	validateProviderDependencies(provider, values, &errs)
	return errs
}

func optionExists(options []ConfigOption, selected string) bool {
	for _, option := range options {
		if option.Value == selected && !option.Disabled {
			return true
		}
	}
	return false
}

func stringSlice(value any) ([]string, bool) {
	if v, ok := value.([]string); ok {
		return v, true
	}
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func validateProviderDependencies(provider ProviderRuntimeConfig, values ConfigValues, errs *[]ConfigValidationError) {
	for _, section := range provider.Sections {
		for _, field := range section.Fields {
			if field.OptionsSource == nil || field.OptionsSource.FilterBy == nil {
				continue
			}
			selected, _ := values[field.Key].(string)
			parentValue, _ := values[field.OptionsSource.FilterBy.Field].(string)
			if selected == "" || parentValue == "" {
				continue
			}
			for _, option := range field.Options {
				if option.Value != selected {
					continue
				}
				metaValue, _ := option.Meta[field.OptionsSource.FilterBy.Path].(string)
				if metaValue != "" && metaValue != parentValue {
					*errs = append(*errs, ConfigValidationError{Path: "config_values." + field.Key, Code: "invalid_option", Message: fmt.Sprintf("%s is not available for %s", selected, parentValue)})
				}
			}
		}
	}
}

func buildMappedArgs(provider ProviderRuntimeConfig, values ConfigValues) []string {
	args := []string{}
	for _, section := range provider.Sections {
		for _, field := range section.Fields {
			if field.Mapping == nil || field.Mapping.Kind != "arg" || field.Mapping.Name == "" {
				continue
			}
			value, ok := values[field.Key]
			if (!ok || value == "") && field.Default != nil {
				value = field.Default
				ok = true
			}
			if !ok || value == "" {
				continue
			}
			for _, rendered := range renderMappedValues(field, value) {
				if field.Mapping.Mode != "" {
					rendered = field.Mapping.Mode + "=" + quoteConfigValue(rendered)
				}
				args = append(args, field.Mapping.Name, rendered)
			}
		}
	}
	return args
}

func quoteConfigValue(value string) string {
	return strconv.Quote(value)
}

func buildMappedEnv(provider ProviderRuntimeConfig, values ConfigValues) map[string]string {
	env := map[string]string{}
	for _, section := range provider.Sections {
		for _, field := range section.Fields {
			if field.Mapping == nil || field.Mapping.Kind != "env" || field.Mapping.Name == "" {
				continue
			}
			value, ok := values[field.Key]
			if (!ok || value == "") && field.Default != nil {
				value = field.Default
				ok = true
			}
			if !ok || value == "" {
				continue
			}
			rendered := renderMappedValues(field, value)
			if len(rendered) > 0 {
				env[field.Mapping.Name] = strings.Join(rendered, ",")
			}
		}
	}
	return env
}

func renderMappedValues(field ConfigField, value any) []string {
	if field.Key == "agent_sub" && value == "default" {
		return nil
	}
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case bool:
		return []string{strconv.FormatBool(v)}
	case float64:
		return []string{strconv.FormatFloat(v, 'f', -1, 64)}
	case int:
		return []string{strconv.Itoa(v)}
	case []string:
		return v
	case []any:
		out := []string{}
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
