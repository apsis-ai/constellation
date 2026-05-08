package mux

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"strconv"
	"strings"
)

// PiParser parses pi --mode json events.
type PiParser struct {
	Callbacks ParserCallbacks
}

func (p *PiParser) Parse(ctx context.Context, sessionID string, responseID string, r io.Reader, ch chan<- ChanEvent) streamResult {
	var res streamResult
	var sb strings.Builder
	var thinking strings.Builder
	thinkingIndex := 0
	thinkingBlockID := ""
	thinkingStarted := false
	emitThinking := func(eventType SessionStreamEventType) {
		if strings.TrimSpace(thinking.String()) == "" {
			return
		}
		if thinkingBlockID == "" {
			thinkingIndex++
			thinkingBlockID = newStableRationaleBlockID(responseID, "pi-thinking-"+strconv.Itoa(thinkingIndex))
		}
		evt := newRationaleSummaryChanEvent(responseID, thinkingBlockID, thinking.String(), eventType)
		if evt.Type == ChanUIBlock {
			ch <- evt
		}
	}
	finishThinking := func() {
		if strings.TrimSpace(thinking.String()) == "" {
			thinking.Reset()
			thinkingBlockID = ""
			thinkingStarted = false
			return
		}
		if !thinkingStarted {
			emitThinking(SSEUIBlockStarted)
			thinkingStarted = true
		}
		emitThinking(SSEUIBlockCompleted)
		thinking.Reset()
		thinkingBlockID = ""
		thinkingStarted = false
	}
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

		if eventType, _ := event["type"].(string); eventType == "session" {
			if id, _ := event["id"].(string); id != "" {
				res.ConversationID = id
			}
			continue
		}

		typeStr, _ := event["type"].(string)
		switch typeStr {
		case "message_update":
			if delta := piThinkingDelta(event); delta != "" {
				thinking.WriteString(delta)
				if !thinkingStarted {
					emitThinking(SSEUIBlockStarted)
					thinkingStarted = true
				} else {
					emitThinking(SSEUIBlockDelta)
				}
			}
			if piThinkingEnded(event) {
				finishThinking()
			}
			if delta := piTextDelta(event); delta != "" {
				finishThinking()
				cleaned := p.Callbacks.ProcessTextWithStatus(sessionID, delta)
				if cleaned != "" {
					processVisibleText(uiScanner, cleaned, ch, &sb)
				}
			}
		case "tool_execution_start", "tool_execution_update", "tool_execution_end":
			toolName, _ := event["toolName"].(string)
			args, _ := event["args"].(map[string]interface{})
			if toolName != "" {
				actionData := map[string]interface{}{"tool": toolName}
				if args != nil {
					actionData["args"] = args
				}
				if jsonBytes, err := json.Marshal(actionData); err == nil {
					ch <- ChanEvent{Type: ChanAction, JSON: string(jsonBytes)}
				}
				p.Callbacks.TrackAction(sessionID, toolName, args)
			}
		case "turn_end":
			finishThinking()
			if usage := piTokenUsage(event); usage > 0 {
				res.TokenUsage = usage
			}
		}
	}
	finishThinking()
	closeVisibleText(uiScanner, ch, &sb)
	if err := scanner.Err(); err != nil {
		log.Printf("pi stream read error: %v", err)
	}
	res.FullText = sb.String()
	return res
}

func piTextDelta(event map[string]interface{}) string {
	ame, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ame == nil {
		return ""
	}
	if typ, _ := ame["type"].(string); typ != "text_delta" {
		return ""
	}
	delta, _ := ame["delta"].(string)
	return delta
}

func piThinkingDelta(event map[string]interface{}) string {
	ame, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ame == nil {
		return ""
	}
	if typ, _ := ame["type"].(string); typ != "thinking_delta" {
		return ""
	}
	delta, _ := ame["delta"].(string)
	return delta
}

func piThinkingEnded(event map[string]interface{}) bool {
	ame, _ := event["assistantMessageEvent"].(map[string]interface{})
	if ame == nil {
		return false
	}
	typ, _ := ame["type"].(string)
	return typ == "thinking_end"
}

func piTokenUsage(event map[string]interface{}) int {
	message, _ := event["message"].(map[string]interface{})
	if message == nil {
		return 0
	}
	usage, _ := message["usage"].(map[string]interface{})
	if usage == nil {
		return 0
	}
	var total int
	for _, key := range []string{"inputTokens", "outputTokens", "cacheCreationInputTokens", "cacheReadInputTokens"} {
		if n, ok := toInt(usage[key]); ok {
			total += n
		}
	}
	return total
}
