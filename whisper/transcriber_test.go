package whisper

import (
	"os"
	"testing"
)

func TestNewTranscriber_BinaryNotFound(t *testing.T) {
	// Set PATH to empty to guarantee binary not found
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent_dir_only")
	t.Setenv("WHISPER_BIN", "")
	defer os.Setenv("PATH", origPath)

	_, err := NewTranscriber()
	if err == nil {
		t.Fatal("expected error when whisper binary not found")
	}
}

func TestNewTranscriber_WithEnvBin(t *testing.T) {
	// Create a fake whisper binary
	tmpDir := t.TempDir()
	fakeBin := tmpDir + "/whisper-test"
	os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0755)

	t.Setenv("WHISPER_BIN", fakeBin)
	t.Setenv("WHISPER_MODEL_PATH", tmpDir+"/fake-model.bin")

	tr, err := NewTranscriber()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.BinPath() != fakeBin {
		t.Errorf("expected bin path %q, got %q", fakeBin, tr.BinPath())
	}
	if tr.ModelPath() == "" {
		t.Error("expected non-empty model path")
	}
}

func TestTranscriber_ModelReady(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := tmpDir + "/whisper-test"
	os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0755)

	modelPath := tmpDir + "/model.bin"

	t.Setenv("WHISPER_BIN", fakeBin)
	t.Setenv("WHISPER_MODEL_PATH", modelPath)

	tr, err := NewTranscriber()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Model not yet created
	if tr.ModelReady() {
		t.Error("expected model not ready when file doesn't exist")
	}

	// Create the model file
	os.WriteFile(modelPath, []byte("fake model"), 0644)
	if !tr.ModelReady() {
		t.Error("expected model ready after creating file")
	}
}

func TestTranscribe_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := tmpDir + "/whisper-test"
	os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0755)

	t.Setenv("WHISPER_BIN", fakeBin)
	t.Setenv("WHISPER_MODEL_PATH", tmpDir+"/model.bin")

	tr, err := NewTranscriber()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = tr.Transcribe("/nonexistent/audio.wav", "en")
	if err == nil {
		t.Fatal("expected error when audio file not found")
	}
}

func TestTranscribe_FileTooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	fakeBin := tmpDir + "/whisper-test"
	os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0755)

	modelPath := tmpDir + "/model.bin"
	os.WriteFile(modelPath, []byte("model"), 0644)

	t.Setenv("WHISPER_BIN", fakeBin)
	t.Setenv("WHISPER_MODEL_PATH", modelPath)

	tr, err := NewTranscriber()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a file larger than MaxFileSize
	bigFile := tmpDir + "/big.wav"
	f, _ := os.Create(bigFile)
	f.Truncate(MaxFileSize + 1)
	f.Close()

	_, err = tr.Transcribe(bigFile, "en")
	if err == nil {
		t.Fatal("expected error for file too large")
	}
}

func TestParseWhisperStdout(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "with timestamps",
			input:  "[00:00.000 --> 00:05.000]  Hello world\n[00:05.000 --> 00:10.000]  How are you",
			expect: "Hello world How are you",
		},
		{
			name:   "plain text",
			input:  "Hello world",
			expect: "Hello world",
		},
		{
			name:   "whisper log lines filtered",
			input:  "whisper_init: loading model\nmain: started\nHello",
			expect: "Hello",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseWhisperStdout(tc.input)
			if result != tc.expect {
				t.Errorf("expected %q, got %q", tc.expect, result)
			}
		})
	}
}
