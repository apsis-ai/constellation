package mux

import (
	"strings"
	"testing"
)

func TestParsePerigeeUIDirectivesStripsValidFence(t *testing.T) {
	input := "Before\n```perigee-ui\n{\"kind\":\"card\",\"schema\":\"choice\",\"schemaVersion\":1,\"id\":\"choose\",\"title\":\"Pick\",\"options\":[{\"id\":\"a\",\"label\":\"A\"}]}\n```\nAfter"

	visible, blocks, invalid := parsePerigeeUIDirectives("m1", input)

	if visible != "Before\n\nAfter" && visible != "Before\nAfter" {
		t.Fatalf("visible = %q", visible)
	}
	if len(invalid) != 0 {
		t.Fatalf("invalid = %d, want 0", len(invalid))
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	block := blocks[0]
	if block.BlockID != "choose" || block.Schema != "choice" || block.Payload["title"] != "Pick" {
		t.Fatalf("unexpected block: %#v", block)
	}
}

func TestParsePerigeeUIDirectivesKeepsInvalidFenceVisible(t *testing.T) {
	input := "Before\n```perigee-ui\n{bad json}\n```\nAfter"

	visible, blocks, invalid := parsePerigeeUIDirectives("m1", input)

	if len(blocks) != 0 {
		t.Fatalf("blocks = %d, want 0", len(blocks))
	}
	if len(invalid) != 1 {
		t.Fatalf("invalid = %d, want 1", len(invalid))
	}
	if visible != input {
		t.Fatalf("invalid directive should remain visible, got %q", visible)
	}
}

func TestParsePerigeeUIDirectivesKeepsUnsupportedSchemaVisible(t *testing.T) {
	input := "```perigee-ui\n{\"kind\":\"card\",\"schema\":\"unknown\",\"schemaVersion\":1,\"id\":\"x\"}\n```"

	visible, blocks, invalid := parsePerigeeUIDirectives("m1", input)

	if len(blocks) != 0 || len(invalid) != 1 || visible != input {
		t.Fatalf("unsupported schema should fall back to text; visible=%q blocks=%d invalid=%d", visible, len(blocks), len(invalid))
	}
}

func TestParsePerigeeUIDirectivesRejectsChoiceWithoutOptions(t *testing.T) {
	input := "```perigee-ui\n{\"kind\":\"card\",\"schema\":\"choice\",\"schemaVersion\":1,\"id\":\"x\",\"title\":\"Pick\"}\n```"

	visible, blocks, invalid := parsePerigeeUIDirectives("m1", input)

	if len(blocks) != 0 || len(invalid) != 1 || visible != input {
		t.Fatalf("choice without options should fall back to text; visible=%q blocks=%d invalid=%d", visible, len(blocks), len(invalid))
	}
}

func TestParsePerigeeUIDirectivesGeneratesStableID(t *testing.T) {
	input := "```perigee-ui\n{\"kind\":\"card\",\"schema\":\"choice\",\"schemaVersion\":1,\"title\":\"Pick\",\"options\":[{\"id\":\"a\",\"label\":\"A\"}]}\n```"

	_, first, invalid := parsePerigeeUIDirectives("m1", input)
	_, second, _ := parsePerigeeUIDirectives("m1", input)

	if len(invalid) != 0 || len(first) != 1 || len(second) != 1 {
		t.Fatalf("unexpected parse result: first=%#v second=%#v invalid=%#v", first, second, invalid)
	}
	if !strings.HasPrefix(first[0].BlockID, "ui_") {
		t.Fatalf("generated id = %q, want ui_ prefix", first[0].BlockID)
	}
	if first[0].BlockID != second[0].BlockID {
		t.Fatalf("generated id should be stable, got %q and %q", first[0].BlockID, second[0].BlockID)
	}
}

func TestUIDirectiveScannerEmitsTextBlockTextInOrder(t *testing.T) {
	ch := make(chan ChanEvent, 3)
	scanner := newUIDirectiveScanner("m1")

	visible := scanner.Process("Before\n```perigee-ui\n{\"kind\":\"card\",\"schema\":\"action_status\",\"schemaVersion\":1,\"id\":\"read\",\"label\":\"Read files\",\"state\":\"completed\"}\n```\nAfter", ch)
	visible += scanner.Close(ch)
	close(ch)

	if visible != "Before\n\nAfter" && visible != "Before\nAfter" {
		t.Fatalf("visible = %q", visible)
	}
	events := []ChanEvent{}
	for evt := range ch {
		events = append(events, evt)
	}
	if len(events) != 3 || events[0].Type != ChanText || events[1].Type != ChanUIBlock || events[2].Type != ChanText {
		t.Fatalf("events out of order: %#v", events)
	}
}

func TestUIDirectiveScannerBuffersSplitFence(t *testing.T) {
	ch := make(chan ChanEvent, 3)
	scanner := newUIDirectiveScanner("m1")

	scanner.Process("Before\n```per", ch)
	scanner.Process("igee-ui\n{\"kind\":\"card\",\"schema\":\"choice\",\"schemaVersion\":1,\"id\":\"choose\",\"title\":\"Pick\",\"options\":[{\"id\":\"a\",\"label\":\"A\"}]}\n```\nAfter", ch)
	scanner.Close(ch)
	close(ch)

	events := []ChanEvent{}
	for evt := range ch {
		events = append(events, evt)
	}
	if len(events) != 3 || events[1].Type != ChanUIBlock {
		t.Fatalf("split fence did not produce ordered UI block: %#v", events)
	}
}
