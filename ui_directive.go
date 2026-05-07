package mux

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var perigeeUIFenceRE = regexp.MustCompile("(?s)```perigee-ui\\s*\\n(.*?)\\n```")

var allowedUIKinds = map[string]struct{}{"text": {}, "card": {}}
var allowedUISchemas = map[string]struct{}{
	"rationale_summary": {},
	"action_status":     {},
	"choice":            {},
}

type parsedUIDirective struct {
	Block UIBlockEvent
	Raw   string
}

type uiDirectiveJSON struct {
	Kind          string                   `json:"kind"`
	Schema        string                   `json:"schema"`
	SchemaVersion int                      `json:"schemaVersion"`
	ID            string                   `json:"id"`
	Title         string                   `json:"title,omitempty"`
	Body          string                   `json:"body,omitempty"`
	Text          string                   `json:"text,omitempty"`
	Label         string                   `json:"label,omitempty"`
	State         string                   `json:"state,omitempty"`
	Summary       string                   `json:"summary,omitempty"`
	Options       []map[string]interface{} `json:"options,omitempty"`
	Presentation  map[string]interface{}   `json:"presentation,omitempty"`
}

func parsePerigeeUIDirectives(messageID, text string) (visible string, blocks []UIBlockEvent, invalid []string) {
	visible = perigeeUIFenceRE.ReplaceAllStringFunc(text, func(match string) string {
		matches := perigeeUIFenceRE.FindStringSubmatch(match)
		if len(matches) != 2 {
			invalid = append(invalid, match)
			return match
		}
		block, err := decodePerigeeUIDirective(messageID, strings.TrimSpace(matches[1]))
		if err != nil {
			invalid = append(invalid, match)
			return match
		}
		blocks = append(blocks, block)
		return ""
	})
	return strings.TrimSpace(visible), blocks, invalid
}

func decodePerigeeUIDirective(messageID, raw string) (UIBlockEvent, error) {
	var directive uiDirectiveJSON
	if err := json.Unmarshal([]byte(raw), &directive); err != nil {
		return UIBlockEvent{}, err
	}
	if directive.Kind == "" || directive.Schema == "" || directive.SchemaVersion <= 0 {
		return UIBlockEvent{}, fmt.Errorf("missing required perigee-ui fields")
	}
	if _, ok := allowedUIKinds[directive.Kind]; !ok {
		return UIBlockEvent{}, fmt.Errorf("unsupported perigee-ui kind %q", directive.Kind)
	}
	if _, ok := allowedUISchemas[directive.Schema]; !ok {
		return UIBlockEvent{}, fmt.Errorf("unsupported perigee-ui schema %q", directive.Schema)
	}
	switch directive.Schema {
	case "rationale_summary":
		if directive.Text == "" {
			return UIBlockEvent{}, fmt.Errorf("rationale_summary requires text")
		}
	case "action_status":
		if directive.Label == "" || directive.State == "" {
			return UIBlockEvent{}, fmt.Errorf("action_status requires label and state")
		}
	case "choice":
		if directive.Title == "" || len(directive.Options) == 0 {
			return UIBlockEvent{}, fmt.Errorf("choice requires title and options")
		}
	}
	blockID := directive.ID
	if blockID == "" {
		sum := sha1.Sum([]byte(messageID + ":" + raw))
		blockID = "ui_" + hex.EncodeToString(sum[:])[:12]
	}
	payload := UIBlockPayload{}
	copyString := func(key, value string) {
		if value != "" {
			payload[key] = value
		}
	}
	copyString("title", directive.Title)
	copyString("body", directive.Body)
	copyString("text", directive.Text)
	copyString("label", directive.Label)
	copyString("state", directive.State)
	copyString("summary", directive.Summary)
	if len(directive.Options) > 0 {
		payload["options"] = directive.Options
	}
	if directive.Presentation != nil {
		payload["presentation"] = directive.Presentation
	}
	return UIBlockEvent{
		ProtocolVersion: "perigee.ui-events.v1",
		MessageID:       messageID,
		BlockID:         blockID,
		Kind:            directive.Kind,
		Schema:          directive.Schema,
		SchemaVersion:   directive.SchemaVersion,
		Payload:         payload,
	}, nil
}

type uiDirectiveScanner struct {
	messageID string
	buffer    string
}

func newUIDirectiveScanner(messageID string) *uiDirectiveScanner {
	return &uiDirectiveScanner{messageID: messageID}
}

func (s *uiDirectiveScanner) Process(text string, ch chan<- ChanEvent) string {
	s.buffer += text
	return s.flush(ch, false)
}

func (s *uiDirectiveScanner) Close(ch chan<- ChanEvent) string {
	return s.flush(ch, true)
}

func (s *uiDirectiveScanner) flush(ch chan<- ChanEvent, final bool) string {
	var visible strings.Builder
	for {
		start := strings.Index(s.buffer, "```perigee-ui")
		if start < 0 {
			if final {
				visible.WriteString(s.buffer)
				s.emitText(s.buffer, ch)
				s.buffer = ""
				return visible.String()
			}
			keep := longestPerigeeFencePrefix(s.buffer)
			emit := s.buffer[:len(s.buffer)-keep]
			if emit != "" {
				visible.WriteString(emit)
				s.emitText(emit, ch)
				s.buffer = s.buffer[len(emit):]
			}
			return visible.String()
		}
		if start > 0 {
			emit := s.buffer[:start]
			visible.WriteString(emit)
			s.emitText(emit, ch)
			s.buffer = s.buffer[start:]
		}
		bodyStart := strings.Index(s.buffer, "\n")
		if bodyStart < 0 {
			if final {
				s.emitText(s.buffer, ch)
				visible.WriteString(s.buffer)
				s.buffer = ""
			}
			return visible.String()
		}
		endRel := strings.Index(s.buffer[bodyStart+1:], "\n```")
		if endRel < 0 {
			if final {
				s.emitText(s.buffer, ch)
				visible.WriteString(s.buffer)
				s.buffer = ""
			}
			return visible.String()
		}
		body := strings.TrimSpace(s.buffer[bodyStart+1 : bodyStart+1+endRel])
		end := bodyStart + 1 + endRel + len("\n```")
		block, err := decodePerigeeUIDirective(s.messageID, body)
		if err != nil {
			raw := s.buffer[:end]
			visible.WriteString(raw)
			s.emitText(raw, ch)
		} else if b, err := json.Marshal(block); err == nil {
			ch <- ChanEvent{Type: ChanUIBlock, JSON: string(b)}
		}
		s.buffer = s.buffer[end:]
	}
}

func (s *uiDirectiveScanner) emitText(text string, ch chan<- ChanEvent) {
	if text != "" {
		ch <- ChanEvent{Type: ChanText, Text: text}
	}
}

func processVisibleText(uiScanner *uiDirectiveScanner, text string, ch chan<- ChanEvent, sb *strings.Builder) {
	if text == "" {
		return
	}
	visible := uiScanner.Process(text, ch)
	sb.WriteString(visible)
}

func closeVisibleText(uiScanner *uiDirectiveScanner, ch chan<- ChanEvent, sb *strings.Builder) {
	if tail := uiScanner.Close(ch); tail != "" {
		sb.WriteString(tail)
	}
}

func longestPerigeeFencePrefix(text string) int {
	marker := "```perigee-ui"
	max := len(marker) - 1
	if len(text) < max {
		max = len(text)
	}
	for n := max; n > 0; n-- {
		if strings.HasSuffix(text, marker[:n]) {
			return n
		}
	}
	return 0
}
