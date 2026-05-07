package mux

import (
	"context"
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
