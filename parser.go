package mux

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"strings"
	"syscall"
	"time"
)

// streamClaudeOutput parses Claude CLI stream-json events.
func (m *Manager) streamClaudeOutput(sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
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
		if t, ok := event["type"].(string); ok {
			switch t {
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
									cleaned := m.processTextWithStatus(sessionID, text)
									if cleaned != "" {
										ch <- ChanEvent{Type: ChanText, Text: cleaned}
										sb.WriteString(cleaned)
									}
								}
							} else if block["type"] == "tool_use" {
								actionData := map[string]interface{}{
									"tool": block["name"],
								}
								if input, ok := block["input"].(map[string]interface{}); ok {
									actionData["args"] = input
								}
								if jsonBytes, err := json.Marshal(actionData); err == nil {
									ch <- ChanEvent{Type: ChanAction, JSON: string(jsonBytes)}
								}
								if name, ok := block["name"].(string); ok {
									input, _ := block["input"].(map[string]interface{})
									m.trackAction(sessionID, name, input)
									displayAs := ""
									if name == "ask_user" {
										displayAs = "hidden"
									}
									m.appendConversation(sessionID, ConversationEntry{
										Role:      "action",
										Agent:     "claude",
										Tool:      name,
										Input:     input,
										DisplayAs: displayAs,
									})
									m.debounceSummary(sessionID)

									// Detect ask_user tool
									if name == "ask_user" {
										question, _ := input["question"].(string)
										if question != "" {
											options := coerceStringSlice(input["options"])
											extra := map[string]interface{}{"question": question}
											if len(options) > 0 {
												extra["options"] = options
											}
											questionJSON, _ := json.Marshal(extra)
											ch <- ChanEvent{Type: ChanAskUser, JSON: string(questionJSON)}
											m.mu.Lock()
											m.askUserPending[sessionID] = AskUserPending{Question: question, Options: options}
											proc, hasProc := m.activeProcesses[sessionID]
											m.mu.Unlock()
											go func() {
												time.Sleep(500 * time.Millisecond)
												if hasProc {
													_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
												}
											}()
										}
									}
								}
							}
						}
					}
				}
			case "result":
				res.FullText = sb.String()
				if sid, ok := event["session_id"].(string); ok {
					res.ConversationID = sid
				}
				if mu, ok := event["modelUsage"].(map[string]interface{}); ok {
					for _, v := range mu {
						usage, ok := v.(map[string]interface{})
						if !ok {
							continue
						}
						inputTokens, _ := toInt(usage["inputTokens"])
						outputTokens, _ := toInt(usage["outputTokens"])
						cacheCreate, _ := toInt(usage["cacheCreationInputTokens"])
						cacheRead, _ := toInt(usage["cacheReadInputTokens"])
						contextWindow, _ := toInt(usage["contextWindow"])
						res.TokenUsage = inputTokens + outputTokens + cacheCreate + cacheRead
						if contextWindow > 0 {
							res.UsagePct = float64(res.TokenUsage) / float64(contextWindow)
						}
						break
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("stream read error: %v", err)
	}
	if res.FullText == "" {
		res.FullText = sb.String()
	}
	return res
}

// streamCodexOutput parses Codex JSONL events produced by `codex exec --json`.
func (m *Manager) streamCodexOutput(sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
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
		case "item.completed":
			if item, ok := event["item"].(map[string]interface{}); ok {
				if itemType, _ := item["type"].(string); itemType == "agent_message" {
					if text, ok := item["text"].(string); ok {
						cleaned := m.processTextWithStatus(sessionID, text)
						if cleaned != "" {
							ch <- ChanEvent{Type: ChanText, Text: cleaned}
							sb.WriteString(cleaned)
						}
					}
				}
			}
		case "item.delta":
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if delta["type"] == "text" {
					if text, ok := delta["text"].(string); ok {
						cleaned := m.processTextWithStatus(sessionID, text)
						if cleaned != "" {
							ch <- ChanEvent{Type: ChanText, Text: cleaned}
							sb.WriteString(cleaned)
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
	if err := scanner.Err(); err != nil {
		log.Printf("codex stream read error: %v", err)
	}
	if res.FullText == "" {
		res.FullText = sb.String()
	}
	return res
}

// streamOpenCodeOutput parses OpenCode JSON events.
func (m *Manager) streamOpenCodeOutput(sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
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
					cleaned := m.processTextWithStatus(sessionID, text)
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
					m.trackAction(sessionID, toolName, input)
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
				m.appendConversation(sessionID, ConversationEntry{
					Role:   "action",
					Agent:  "opencode",
					Tool:   toolName,
					Input:  actionInput,
					Result: actionResult,
				})
				m.debounceSummary(sessionID)
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

		// Extract sessionID from events for resume support
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

// streamCursorOutput parses Cursor CLI stream-json NDJSON events.
func (m *Manager) streamCursorOutput(sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
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
								cleaned := m.processTextWithStatus(sessionID, text)
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
						m.trackAction(sessionID, toolName, toolArgs)
						m.appendConversation(sessionID, ConversationEntry{
							Role:  "action",
							Agent: "cursor",
							Tool:  toolName,
							Input: toolArgs,
						})
						m.debounceSummary(sessionID)
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
						m.appendConversation(sessionID, ConversationEntry{
							Role:   "action",
							Agent:  "cursor",
							Tool:   toolName + "_result",
							Result: resultText,
						})
					}
				}
			}
			// Handle non-MCP tool calls (built-in Cursor tools)
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
					m.trackAction(sessionID, fnName, fnArgs)
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
