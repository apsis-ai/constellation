package whisper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultModel = "ggml-medium.bin"
	MaxFileSize  = 10 << 20 // 10MB
	BaseTimeout  = 30 * time.Second
	TimeoutPerMB = 20 * time.Second  // scale timeout with file size
	MaxTimeout   = 180 * time.Second // cap at 3 minutes for large files
)

// Transcriber wraps the whisper.cpp CLI for local speech-to-text.
type Transcriber struct {
	binPath   string
	modelPath string
}

// NewTranscriber creates a Transcriber, resolving the whisper binary and model paths.
// Binary resolution order: WHISPER_BIN env var -> "whisper-cpp" on PATH -> "whisper" on PATH -> "main" on PATH.
// Model resolution: WHISPER_MODEL_PATH env var -> ~/.ai-remote-screen/models/<model>.
func NewTranscriber() (*Transcriber, error) {
	bin := findWhisperBin()
	if bin == "" {
		return nil, fmt.Errorf("whisper binary not found: install whisper.cpp and ensure it is on PATH, or set WHISPER_BIN")
	}

	modelName := os.Getenv("WHISPER_MODEL")
	if modelName == "" {
		modelName = DefaultModel
	}
	if !strings.HasSuffix(modelName, ".bin") {
		modelName = "ggml-" + modelName + ".bin"
	}

	modelPath := os.Getenv("WHISPER_MODEL_PATH")
	if modelPath == "" {
		home, _ := os.UserHomeDir()
		modelPath = filepath.Join(home, ".ai-remote-screen", "models", modelName)
	}

	return &Transcriber{
		binPath:   bin,
		modelPath: modelPath,
	}, nil
}

func findWhisperBin() string {
	if env := os.Getenv("WHISPER_BIN"); env != "" {
		if _, err := exec.LookPath(env); err == nil {
			return env
		}
	}
	for _, name := range []string{"whisper-cpp", "whisper", "main"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// BinPath returns the configured whisper binary path.
func (t *Transcriber) BinPath() string {
	return t.binPath
}

// ModelReady returns true if the model file exists at the configured path.
func (t *Transcriber) ModelReady() bool {
	info, err := os.Stat(t.modelPath)
	return err == nil && !info.IsDir()
}

// ModelPath returns the configured model file path.
func (t *Transcriber) ModelPath() string {
	return t.modelPath
}

// Transcribe runs whisper.cpp on the given audio file and returns the transcribed text.
// language can be "auto", "en", "pt", or empty (defaults to auto-detect).
// The audio file is first converted to WAV via ffmpeg (whisper.cpp requires WAV input).
func (t *Transcriber) Transcribe(audioPath, language string) (string, error) {
	if _, err := os.Stat(audioPath); os.IsNotExist(err) {
		return "", fmt.Errorf("file not found: %s", audioPath)
	}

	info, err := os.Stat(audioPath)
	if err != nil {
		return "", fmt.Errorf("stat audio file: %w", err)
	}
	if info.Size() > MaxFileSize {
		return "", fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), MaxFileSize)
	}

	if !t.ModelReady() {
		return "", fmt.Errorf("whisper model not found at %s: download it or set WHISPER_MODEL_PATH", t.modelPath)
	}

	// Convert to WAV (whisper.cpp requires WAV input)
	wavPath, err := t.convertToWAV(audioPath)
	if err != nil {
		return "", fmt.Errorf("audio conversion failed: %w", err)
	}
	defer os.Remove(wavPath)

	// Scale timeout with file size
	sizeMB := float64(info.Size()) / (1 << 20)
	timeout := BaseTimeout + time.Duration(sizeMB*float64(TimeoutPerMB))
	if timeout > MaxTimeout {
		timeout = MaxTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := []string{
		"-m", t.modelPath,
		"-f", wavPath,
		"--no-timestamps",
		"--output-txt",
		"-of", wavPath + ".out",
	}
	if language == "" || language == "auto" {
		args = append(args, "--language", "auto")
	} else {
		args = append(args, "--language", language)
	}

	cmd := exec.CommandContext(ctx, t.binPath, args...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("transcription timeout after %s", timeout)
	}
	if err != nil {
		return "", fmt.Errorf("whisper failed: %w\noutput: %s", err, string(output))
	}

	// Read the output text file
	txtPath := wavPath + ".out.txt"
	txt, err := os.ReadFile(txtPath)
	if err != nil {
		// Fallback: parse stdout for transcription
		return parseWhisperStdout(string(output)), nil
	}
	os.Remove(txtPath) // cleanup

	result := strings.TrimSpace(string(txt))
	if result == "" {
		return "", fmt.Errorf("transcription empty: could not understand audio")
	}
	return result, nil
}

// convertToWAV uses ffmpeg to convert any audio format to 16kHz mono WAV.
func (t *Transcriber) convertToWAV(inputPath string) (string, error) {
	wavPath := inputPath + ".wav"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", inputPath,
		"-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", wavPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg conversion failed: %w\noutput: %s", err, string(output))
	}
	return wavPath, nil
}

func parseWhisperStdout(output string) string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "whisper_") || strings.HasPrefix(line, "main:") {
			continue
		}
		// Strip timestamp prefix like "[00:00.000 --> 00:05.000]  "
		if strings.HasPrefix(line, "[") {
			if idx := strings.Index(line, "]"); idx >= 0 {
				text := strings.TrimSpace(line[idx+1:])
				if text != "" {
					lines = append(lines, text)
				}
			}
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}
