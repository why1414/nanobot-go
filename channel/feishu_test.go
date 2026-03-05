package channel

import (
	"testing"
)

// TestFeishuMarkSeen verifies the deduplication logic.
func TestFeishuMarkSeen(t *testing.T) {
	ch := NewFeishuChannel(FeishuConfig{
		AppID:     "test",
		AppSecret: "secret",
	}, nil)

	// First time: should be seen as new.
	if !ch.markSeen("msg-001") {
		t.Error("expected markSeen to return true for new message")
	}

	// Second time: duplicate, should return false.
	if ch.markSeen("msg-001") {
		t.Error("expected markSeen to return false for duplicate message")
	}

	// Different ID: should be new.
	if !ch.markSeen("msg-002") {
		t.Error("expected markSeen to return true for different message")
	}
}

// TestFeishuMarkSeenEviction verifies that the cache evicts old entries when
// it exceeds maxDedupSize.
func TestFeishuMarkSeenEviction(t *testing.T) {
	ch := NewFeishuChannel(FeishuConfig{}, nil)

	// Fill to capacity.
	for i := 0; i < maxDedupSize; i++ {
		id := "msg-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		ch.markSeen(id)
	}
	if len(ch.dedupKeys) != maxDedupSize {
		t.Fatalf("expected %d entries, got %d", maxDedupSize, len(ch.dedupKeys))
	}

	// Adding one more should evict the oldest.
	first := ch.dedupKeys[0]
	ch.markSeen("msg-extra")
	if _, exists := ch.dedupSet[first]; exists {
		t.Error("expected oldest entry to be evicted")
	}
	if len(ch.dedupKeys) != maxDedupSize {
		t.Fatalf("expected cache size to remain %d after eviction, got %d", maxDedupSize, len(ch.dedupKeys))
	}
}

// TestBuildCardElements verifies that markdown headings are converted to div
// elements and plain text becomes markdown elements.
func TestBuildCardElements(t *testing.T) {
	ch := NewFeishuChannel(FeishuConfig{}, nil)

	elements := ch.buildCardElements("## Hello\nWorld")
	if len(elements) == 0 {
		t.Fatal("expected at least one element")
	}

	// First element should be a div (heading).
	first := elements[0]
	if first["tag"] != "div" {
		t.Errorf("expected first element tag 'div', got %q", first["tag"])
	}

	// Last element should be a markdown element containing "World".
	last := elements[len(elements)-1]
	if last["tag"] != "markdown" {
		t.Errorf("expected last element tag 'markdown', got %q", last["tag"])
	}
}

// TestExtractPostText verifies text extraction from a Feishu post message.
func TestExtractPostText(t *testing.T) {
	content := map[string]any{
		"zh_cn": map[string]any{
			"title": "Test Post",
			"content": []any{
				[]any{
					map[string]any{"tag": "text", "text": "Hello"},
					map[string]any{"tag": "text", "text": "World"},
				},
			},
		},
	}
	got := extractPostText(content)
	want := "Test Post Hello World"
	if got != want {
		t.Errorf("extractPostText: got %q, want %q", got, want)
	}
}
