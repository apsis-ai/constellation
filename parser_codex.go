package mux

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
)

// CodexParser parses Codex JSONL events produced by `codex exec --json`.
type CodexParser struct {
	Callbacks ParserCallbacks
}

func (p *CodexParser) Parse(ctx context.Context, sessionID string, responseID string, r io.Reader, ch chan<- ChanEvent) streamResult {
	var res streamResult
	var sb strings.Builder
	uiScanner := newUIDirectiveScanner(responseID)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			processVisibleText(uiScanner, line+"\n", ch, &sb)
			continue
		}
		typeStr, _ := event["type"].(string)
		switch typeStr {
		case "item.completed":
			if item, ok := event["item"].(map[string]interface{}); ok {
				if itemType, _ := item["type"].(string); itemType == "agent_message" {
					if text, ok := item["text"].(string); ok {
						cleaned := p.Callbacks.ProcessTextWithStatus(sessionID, text)
						if cleaned != "" {
							processVisibleText(uiScanner, cleaned, ch, &sb)
						}
					}
				}
			}
		case "item.delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if delta["type"] == "text" {
					if text, ok := delta["text"].(string); ok {
						cleaned := p.Callbacks.ProcessTextWithStatus(sessionID, text)
						if cleaned != "" {
							processVisibleText(uiScanner, cleaned, ch, &sb)
						}
					}
				}
			}
		case "turn.completed":
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				total := 0
				for _, key := range []string{"input_tokens", "cached_input_tokens", "output_tokens"} {
					if val, exists := usage[key]; exists {
						if n, ok := toInt(val); ok {
							total += n
						}
					}
				}
				res.TokenUsage = total
			}
		}
	}
	closeVisibleText(uiScanner, ch, &sb)
	if err := scanner.Err(); err != nil {
		log.Printf("codex stream read error: %v", err)
	}
	if res.FullText == "" {
		res.FullText = sb.String()
	}
	return res
}
