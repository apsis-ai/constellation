package mux

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
)

// ClaudeParser parses Claude CLI stream-json events.
type ClaudeParser struct {
	Callbacks ParserCallbacks
}

func (p *ClaudeParser) Parse(ctx context.Context, sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
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
									cleaned := p.Callbacks.ProcessTextWithStatus(sessionID, text)
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
									p.Callbacks.TrackAction(sessionID, name, input)
									displayAs := ""
									if name == "ask_user" {
										displayAs = "hidden"
									}
									p.Callbacks.AppendConversation(sessionID, ConversationEntry{
										Role:      "action",
										Agent:     "claude",
										Tool:      name,
										Input:     input,
										DisplayAs: displayAs,
									})
									p.Callbacks.DebounceSummary(sessionID)

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
											if p.Callbacks.HandleAskUser != nil {
												p.Callbacks.HandleAskUser(sessionID, AskUserPending{Question: question, Options: options})
											}
											if p.Callbacks.KillProcess != nil {
												p.Callbacks.KillProcess(sessionID)
											}
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
