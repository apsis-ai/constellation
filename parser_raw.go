package mux

import (
	"bufio"
	"context"
	"io"
	"log"
	"strings"
)

// RawParser is a passthrough parser for unknown/custom CLI agents ("other" parser type).
// It treats every line as plain text — no structured event parsing.
type RawParser struct {
	Callbacks ParserCallbacks
}

func (p *RawParser) Parse(ctx context.Context, sessionID string, responseID string, r io.Reader, ch chan<- ChanEvent) streamResult {
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
		cleaned := p.Callbacks.ProcessTextWithStatus(sessionID, line+"\n")
		if cleaned != "" {
			processVisibleText(uiScanner, cleaned, ch, &sb)
		}
	}
	closeVisibleText(uiScanner, ch, &sb)
	if err := scanner.Err(); err != nil {
		log.Printf("raw stream read error: %v", err)
	}
	res.FullText = sb.String()
	return res
}
