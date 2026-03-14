package mux

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
)

// CursorParser parses Cursor CLI stream-json NDJSON events.
type CursorParser struct {
	Callbacks ParserCallbacks
}

func (p *CursorParser) Parse(ctx context.Context, sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
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
		switch typeStr {
		case "assistant":
			if msg, ok := event["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].([]interface{}); ok {
					for _, c := range content {
						block, ok := c.(map[string]interface{})
						if !ok {
							continue
						}
						if block["type"] == "text" {
							if text, ok := block["text"].(string); ok {
								cleaned := p.Callbacks.ProcessTextWithStatus(sessionID, text)
								if cleaned != "" {
									ch <- ChanEvent{Type: ChanText, Text: cleaned}
									sb.WriteString(cleaned)
								}
							}
						}
					}
				}
			}
		case "tool_call":
			subtype, _ := event["subtype"].(string)
			tc, _ := event["tool_call"].(map[string]interface{})
			if tc == nil {
				break
			}
			if mcp, ok := tc["mcpToolCall"].(map[string]interface{}); ok {
				mcpArgs, _ := mcp["args"].(map[string]interface{})
				if mcpArgs == nil {
					break
				}
				toolName, _ := mcpArgs["toolName"].(string)
				if toolName == "" {
					toolName, _ = mcpArgs["name"].(string)
				}

				if subtype == "started" {
					toolArgs, _ := mcpArgs["args"].(map[string]interface{})
					actionData := map[string]interface{}{"tool": toolName}
					if toolArgs != nil {
						actionData["args"] = toolArgs
					}
					if jsonBytes, err := json.Marshal(actionData); err == nil {
						ch <- ChanEvent{Type: ChanAction, JSON: string(jsonBytes)}
					}
					if toolName != "" {
						p.Callbacks.TrackAction(sessionID, toolName, toolArgs)
						p.Callbacks.AppendConversation(sessionID, ConversationEntry{
							Role:  "action",
							Agent: "cursor",
							Tool:  toolName,
							Input: toolArgs,
						})
						p.Callbacks.DebounceSummary(sessionID)
					}
				} else if subtype == "completed" {
					var resultText string
					if result, ok := mcp["result"].(map[string]interface{}); ok {
						if success, ok := result["success"].(map[string]interface{}); ok {
							if content, ok := success["content"].([]interface{}); ok {
								for _, item := range content {
									itemMap, ok := item.(map[string]interface{})
									if !ok {
										continue
									}
									if textObj, ok := itemMap["text"].(map[string]interface{}); ok {
										if t, ok := textObj["text"].(string); ok && resultText == "" {
											if len(t) > 200 {
												resultText = t[:200] + "..."
											} else {
												resultText = t
											}
										}
									}
								}
							}
						}
					}
					if resultText != "" {
						p.Callbacks.AppendConversation(sessionID, ConversationEntry{
							Role:   "action",
							Agent:  "cursor",
							Tool:   toolName + "_result",
							Result: resultText,
						})
					}
				}
			}
			if fn, ok := tc["function"].(map[string]interface{}); ok && subtype == "started" {
				fnName, _ := fn["name"].(string)
				fnArgsStr, _ := fn["arguments"].(string)
				var fnArgs map[string]interface{}
				_ = json.Unmarshal([]byte(fnArgsStr), &fnArgs)
				actionData := map[string]interface{}{"tool": fnName}
				if fnArgs != nil {
					actionData["args"] = fnArgs
				}
				if jsonBytes, err := json.Marshal(actionData); err == nil {
					ch <- ChanEvent{Type: ChanAction, JSON: string(jsonBytes)}
				}
				if fnName != "" {
					p.Callbacks.TrackAction(sessionID, fnName, fnArgs)
				}
			}
		case "result":
			res.FullText = sb.String()
			if sid, ok := event["session_id"].(string); ok {
				res.ConversationID = sid
			}
			if usage, ok := event["usage"].(map[string]interface{}); ok {
				inputTokens, _ := toInt(usage["inputTokens"])
				outputTokens, _ := toInt(usage["outputTokens"])
				cacheRead, _ := toInt(usage["cacheReadTokens"])
				cacheWrite, _ := toInt(usage["cacheWriteTokens"])
				res.TokenUsage = inputTokens + outputTokens + cacheRead + cacheWrite
			}
		case "error":
			if isErr, ok := event["is_error"].(bool); ok && isErr {
				if msg, ok := event["result"].(string); ok && msg != "" {
					ch <- ChanEvent{Type: ChanText, Text: "\n[Error: " + msg + "]\n"}
					sb.WriteString("\n[Error: " + msg + "]\n")
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("cursor stream read error: %v", err)
	}
	if res.FullText == "" {
		res.FullText = sb.String()
	}
	return res
}
