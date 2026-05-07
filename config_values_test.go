package mux

import "testing"

func TestConfigValuesJSONRoundTrip(t *testing.T) {
	values := map[string]any{"model": "openai/gpt-5", "effort": "high"}

	raw := MarshalConfigValues(values)
	decoded := UnmarshalConfigValues(raw)

	if decoded["model"] != "openai/gpt-5" || decoded["effort"] != "high" {
		t.Fatalf("unexpected decoded values: %#v", decoded)
	}
}

func TestValidateConfigValuesRejectsInvalidNestedProviderModel(t *testing.T) {
	provider := ProviderRuntimeConfig{ID: "pi", Status: ProviderStatusReady, Sections: []ConfigSection{{ID: "provider", Fields: []ConfigField{{Key: "upstream_provider", Type: FieldTypeSelect, Required: true, Options: []ConfigOption{{Value: "openai", Label: "OpenAI"}}}}}, {ID: "model", Fields: []ConfigField{{Key: "model", Type: FieldTypeSelect, Required: true, OptionsSource: &OptionsSource{FilterBy: &FieldFilter{Field: "upstream_provider", Path: "provider"}}, Options: []ConfigOption{{Value: "anthropic/claude", Label: "Claude", Meta: map[string]any{"provider": "anthropic"}}}}}}}}

	errs := ValidateConfigValues(provider, ConfigValues{"upstream_provider": "openai", "model": "anthropic/claude"})

	if len(errs) == 0 || errs[0].Code != "invalid_option" {
		t.Fatalf("expected invalid option, got %#v", errs)
	}
}

func TestBuildMappedArgsUsesFieldMappings(t *testing.T) {
	provider := ProviderRuntimeConfig{Sections: []ConfigSection{{ID: "model", Fields: []ConfigField{{Key: "model", Type: FieldTypeSelect, Mapping: &ExecutionMapping{Kind: "arg", Name: "--model"}}}}, {ID: "effort", Fields: []ConfigField{{Key: "effort", Type: FieldTypeSelect, Mapping: &ExecutionMapping{Kind: "arg", Name: "--thinking"}}}}}}

	args := buildMappedArgs(provider, ConfigValues{"model": "openai/gpt-5", "effort": "high"})

	want := []string{"--model", "openai/gpt-5", "--thinking", "high"}
	assertStringSliceEqual(t, args, want)
}

func TestBuildMappedArgsSupportsConfigOverrideMappings(t *testing.T) {
	provider := ProviderRuntimeConfig{Sections: []ConfigSection{{ID: "effort", Fields: []ConfigField{{Key: "effort", Type: FieldTypeSelect, Mapping: &ExecutionMapping{Kind: "arg", Name: "-c", Mode: "model_reasoning_effort"}}}}}}

	args := buildMappedArgs(provider, ConfigValues{"effort": "high"})

	want := []string{"-c", "model_reasoning_effort=\"high\""}
	assertStringSliceEqual(t, args, want)
}

func assertStringSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %#v, got %#v", want, got)
		}
	}
}
