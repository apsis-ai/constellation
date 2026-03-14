package mux

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
)

// OpenCodeParser parses OpenCode JSON events.
type OpenCodeParser struct {
	Callbacks ParserCallbacks
}

func (p *OpenCodeParser) Parse(ctx context.Context, sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
	var res streamResult
	var sb strings.Builder
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			ch <- ChanEvent{Type: ChanText, Text: line + "\n"}
			sb.WriteString(line + "\n")
			continue
		}
		typeStr, _ := event["type"].(string)
		part, _ := event["part"].(map[string]interface{})

		switch typeStr {
		case "text":
			if part != nil {
				if text, ok := part["text"].(string); ok {
					cleaned := p.Callbacks.ProcessTextWithStatus(sessionID, text)
					if cleaned != "" {
						ch <- ChanEvent{Type: ChanText, Text: cleaned}
						sb.WriteString(cleaned)
					}
				}
			}
		case "tool_use":
			if part != nil {
				toolName, _ := part["tool"].(string)
				state, _ := part["state"].(map[string]interface{})
				actionData := map[string]interface{}{"tool": toolName}
				if state != nil {
					if input, ok := state["input"].(map[string]interface{}); ok {
						actionData["args"] = input
					}
				}
				if jsonBytes, err := json.Marshal(actionData); err == nil {
					ch <- ChanEvent{Type: ChanAction, JSON: string(jsonBytes)}
				}
				if toolName != "" {
					var input map[string]interface{}
					if state != nil {
						input, _ = state["input"].(map[string]interface{})
					}
					p.Callbacks.TrackAction(sessionID, toolName, input)
				}
				var actionInput map[string]interface{}
				var actionResult string
				if state != nil {
					if input, ok := state["input"].(map[string]interface{}); ok {
						actionInput = input
					}
					if output, ok := state["output"].(string); ok {
						if len(output) > 200 {
							actionResult = output[:200] + "..."
						} else {
							actionResult = output
						}
					}
				}
				p.Callbacks.AppendConversation(sessionID, ConversationEntry{
					Role:   "action",
					Agent:  "opencode",
					Tool:   toolName,
					Input:  actionInput,
					Result: actionResult,
				})
				p.Callbacks.DebounceSummary(sessionID)
			}
		case "step_finish":
			if part != nil {
				if tokens, ok := part["tokens"].(map[string]interface{}); ok {
					total, _ := toInt(tokens["total"])
					res.TokenUsage = total
				}
			}
		case "error":
			if errData, ok := event["error"].(map[string]interface{}); ok {
				msg := ""
				if direct, ok := errData["message"].(string); ok {
					msg = direct
				}
				if msg == "" {
					if nested, ok := errData["data"].(map[string]interface{}); ok {
						if nestedMsg, ok := nested["message"].(string); ok {
							msg = nestedMsg
						}
					}
				}
				if msg == "" {
					if name, ok := errData["name"].(string); ok && name != "" {
						msg = name
					}
				}
				if msg != "" {
					ch <- ChanEvent{Type: ChanText, Text: "\n[Error: " + msg + "]\n"}
					sb.WriteString("\n[Error: " + msg + "]\n")
				}
			}
		}

		if sid, ok := event["sessionID"].(string); ok && res.ConversationID == "" {
			res.ConversationID = sid
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("opencode stream read error: %v", err)
	}
	if res.FullText == "" {
		res.FullText = sb.String()
	}
	return res
}
