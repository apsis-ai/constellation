package mux

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderFileConfigValidateAcceptsValidConfig(t *testing.T) {
	cfg := ProviderFileConfig{ID: "pi", Name: "Pi", Binary: "pi", ParserType: "pi", Enabled: true, Sections: []ConfigSection{{ID: "model", Label: "Model", Fields: []ConfigField{{Key: "model", Type: FieldTypeSelect, Label: "Model", Required: true}}}}}

	errs := cfg.Validate()

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %#v", errs)
	}
}

func TestProviderFileConfigValidateRejectsDuplicateFields(t *testing.T) {
	cfg := ProviderFileConfig{ID: "pi", Name: "Pi", Binary: "pi", ParserType: "pi", Sections: []ConfigSection{{ID: "a", Fields: []ConfigField{{Key: "model", Type: FieldTypeSelect}, {Key: "model", Type: FieldTypeSelect}}}}}

	errs := cfg.Validate()

	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}
	if errs[len(errs)-1].Code != "duplicate" {
		t.Fatalf("expected duplicate error, got %#v", errs)
	}
}

func TestProviderFileStoreSeedAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewProviderFileStore(dir)
	builtin := CLIProviderConfig{ProviderID: "pi", Name: "Pi", Binary: "pi", ParserType: "pi", ModelFlag: "--model", Models: []string{"openai/gpt-5"}}

	if err := store.SeedBuiltins(context.Background(), []CLIProviderConfig{builtin}); err != nil {
		t.Fatal(err)
	}
	configs, errs, err := store.Load(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %#v", errs)
	}
	if len(configs) != 1 || configs[0].ID != "pi" {
		t.Fatalf("unexpected configs: %#v", configs)
	}
	if configs[0].Sections[0].Fields[0].Options[0].Value != "openai/gpt-5" {
		t.Fatalf("static model option not preserved")
	}
}

func TestProviderFileStoreSeedBuiltinsBackfillsMissingBuiltinSections(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	store := NewProviderFileStore(dir)
	legacyCodex := ProviderFileConfig{ID: "codex", Name: "Codex", Binary: "codex", ParserType: "codex", Enabled: true, Sections: []ConfigSection{{ID: "model", Label: "Model", Fields: []ConfigField{{Key: "model", Type: FieldTypeSelect, Label: "Model"}}}}}
	data, err := json.Marshal(legacyCodex)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := store.SeedBuiltins(context.Background(), BuiltinCLIConfigs()); err != nil {
		t.Fatal(err)
	}
	configs, errs, err := store.Load(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %#v", errs)
	}
	var codex ProviderFileConfig
	for _, cfg := range configs {
		if cfg.ID == "codex" {
			codex = cfg
			break
		}
	}
	for _, section := range codex.Sections {
		if section.ID == "effort" {
			return
		}
	}
	t.Fatal("expected existing codex provider file to be backfilled with effort section")
}

func TestCLIProviderConfigFromProviderFileBackfillsPiPromptSeparator(t *testing.T) {
	cfg := CLIProviderConfigFromProviderFile(ProviderFileConfig{ID: "pi", Name: "Pi", Binary: "pi", ParserType: "pi", Execution: ProviderExecutionConfig{BaseArgs: []string{"--mode", "json"}}})

	provider := NewCLIProvider(cfg, NewParserRegistry())
	args := provider.BuildArgs(ProviderRequest{Prompt: "hello"})

	for _, arg := range args {
		if arg == "--" {
			t.Fatalf("expected legacy pi provider file to omit prompt separator, got %#v", args)
		}
	}
}

func TestProviderFileStoreSeedBuiltinsCorrectsCodexEffortMapping(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	store := NewProviderFileStore(dir)
	legacyCodex := ProviderFileConfig{ID: "codex", Name: "Codex", Binary: "codex", ParserType: "codex", Enabled: true, Execution: ProviderExecutionConfig{EffortFlag: "--effort"}, Sections: []ConfigSection{{ID: "effort", Label: "Effort", Fields: []ConfigField{{Key: "effort", Type: FieldTypeSelect, Label: "Effort", Mapping: &ExecutionMapping{Kind: "arg", Name: "--effort"}}}}}}
	data, err := json.Marshal(legacyCodex)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	if err := store.SeedBuiltins(context.Background(), BuiltinCLIConfigs()); err != nil {
		t.Fatal(err)
	}
	configs, errs, err := store.Load(context.Background())

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %#v", errs)
	}
	var codex ProviderFileConfig
	for _, cfg := range configs {
		if cfg.ID == "codex" {
			codex = cfg
			break
		}
	}
	if codex.Execution.EffortFlag != "-c" {
		t.Fatalf("expected codex effort execution flag to be corrected, got %q", codex.Execution.EffortFlag)
	}
	field := findField(configs, "codex", "effort")
	if field == nil || field.Mapping == nil || field.Mapping.Name != "-c" || field.Mapping.Mode != "model_reasoning_effort" {
		t.Fatalf("expected codex effort mapping to be corrected, got %#v", field)
	}
}

func TestProviderFileConfigFromCLI_CodexExposesEffortOptions(t *testing.T) {
	// Arrange
	var codex CLIProviderConfig
	for _, cfg := range BuiltinCLIConfigs() {
		if cfg.ProviderID == "codex" {
			codex = cfg
			break
		}
	}

	// Act
	fileConfig := ProviderFileConfigFromCLI(codex)

	// Assert
	var effortField *ConfigField
	for si := range fileConfig.Sections {
		for fi := range fileConfig.Sections[si].Fields {
			field := &fileConfig.Sections[si].Fields[fi]
			if field.Key == "effort" {
				effortField = field
			}
		}
	}
	if effortField == nil {
		t.Fatal("expected codex provider to expose an effort field")
	}
	if effortField.Mapping == nil || effortField.Mapping.Name != "-c" || effortField.Mapping.Mode != "model_reasoning_effort" {
		t.Fatalf("expected effort to map to Codex config override, got %#v", effortField.Mapping)
	}
	if len(effortField.Options) == 0 {
		t.Fatal("expected codex effort field to include options")
	}
}

func findField(configs []ProviderFileConfig, providerID string, fieldKey string) *ConfigField {
	for ci := range configs {
		if configs[ci].ID != providerID {
			continue
		}
		for si := range configs[ci].Sections {
			for fi := range configs[ci].Sections[si].Fields {
				field := &configs[ci].Sections[si].Fields[fi]
				if field.Key == fieldKey {
					return field
				}
			}
		}
	}
	return nil
}

func TestRuntimeConfigServiceListReturnsStaticSections(t *testing.T) {
	dir := t.TempDir()
	store := NewProviderFileStore(dir)
	cfg := CLIProviderConfig{ProviderID: "pi", Name: "Pi", Binary: "sh", ParserType: "pi", ModelFlag: "--model", Models: []string{"openai/gpt-5"}}

	if err := store.SeedBuiltins(context.Background(), []CLIProviderConfig{cfg}); err != nil {
		t.Fatal(err)
	}
	runtime, errs, err := NewRuntimeConfigService(store).List(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %#v", errs)
	}
	if len(runtime) != 1 {
		t.Fatalf("expected one runtime config, got %d", len(runtime))
	}
	field := runtime[0].Sections[0].Fields[0]
	if field.Options[0].Value != "openai/gpt-5" {
		t.Fatalf("expected static option in runtime config, got %#v", field.Options)
	}
}
