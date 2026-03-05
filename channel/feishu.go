// Package channel provides chat-platform integrations.
//
// feishu.go implements the Feishu/Lark channel using WebSocket long connection
// via the lark-oapi Go SDK. No public IP or webhook endpoint is required.
package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/libo/nanobot-go/bus"
)

// FeishuConfig holds the configuration for the Feishu channel.
type FeishuConfig struct {
	AppID             string
	AppSecret         string
	EncryptKey        string
	VerificationToken string
	AllowFrom         []string // empty means allow everyone
}

// FeishuChannel is a Feishu/Lark bot channel that receives messages over a
// WebSocket long connection (no public IP required) and sends interactive
// card replies.
type FeishuChannel struct {
	BaseChannel
	cfg    FeishuConfig
	client *lark.Client

	// dedup cache: holds up to maxDedupSize message IDs
	dedupMu   sync.Mutex
	dedupKeys []string
	dedupSet  map[string]struct{}
}

const (
	feishuChannelName = "feishu"
	maxDedupSize      = 1000
)

// NewFeishuChannel constructs a FeishuChannel. It does NOT start the WebSocket
// connection; call Start to begin receiving events.
func NewFeishuChannel(cfg FeishuConfig, b *bus.MessageBus) *FeishuChannel {
	return &FeishuChannel{
		BaseChannel: NewBaseChannel(b, cfg.AllowFrom),
		cfg:         cfg,
		dedupSet:    make(map[string]struct{}),
	}
}

// Name returns "feishu".
func (f *FeishuChannel) Name() string { return feishuChannelName }

// Start initialises the Feishu client and connects via WebSocket. It blocks
// until ctx is cancelled.
func (f *FeishuChannel) Start(ctx context.Context) error {
	if f.cfg.AppID == "" || f.cfg.AppSecret == "" {
		return fmt.Errorf("feishu: app_id and app_secret are required")
	}

	// Build the REST client (used to send messages and reactions).
	f.client = lark.NewClient(f.cfg.AppID, f.cfg.AppSecret)

	// Build event dispatcher.
	eventDispatcher := dispatcher.NewEventDispatcher(
		f.cfg.VerificationToken,
		f.cfg.EncryptKey,
	).OnP2MessageReceiveV1(func(fCtx context.Context, event *larkim.P2MessageReceiveV1) error {
		return f.onMessageReceive(ctx, event)
	})

	// Build the WebSocket client.
	wsClient := larkws.NewClient(
		f.cfg.AppID,
		f.cfg.AppSecret,
		larkws.WithEventHandler(eventDispatcher),
	)

	slog.Info("feishu: starting WebSocket long connection")

	// Run the WebSocket client in a separate goroutine with reconnect loop.
	errCh := make(chan error, 1)
	go func() {
		for {
			if err := wsClient.Start(ctx); err != nil {
				if ctx.Err() != nil {
					// Normal shutdown.
					return
				}
				slog.Warn("feishu: WebSocket error, retrying in 5s", "error", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
				continue
			}
			return
		}
	}()

	slog.Info("feishu: WebSocket long connection established")

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

// Send delivers an outbound message as a Feishu interactive card. If
// msg.Content is empty nothing is sent.
func (f *FeishuChannel) Send(ctx context.Context, msg *bus.OutboundMessage) error {
	if f.client == nil {
		return fmt.Errorf("feishu: client not initialised")
	}
	if msg.Content == "" {
		return nil
	}

	receiveIDType := "open_id"
	if strings.HasPrefix(msg.ChatID, "oc_") {
		receiveIDType = "chat_id"
	}

	card := map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"elements": f.buildCardElements(msg.Content),
	}
	cardJSON, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("feishu: marshal card: %w", err)
	}

	return f.sendMessage(ctx, receiveIDType, msg.ChatID, "interactive", string(cardJSON))
}

// ---------------------------------------------------------------------------
// Incoming event handling
// ---------------------------------------------------------------------------

// onMessageReceive handles the im.message.receive_v1 event.
func (f *FeishuChannel) onMessageReceive(ctx context.Context, data *larkim.P2MessageReceiveV1) error {
	if data.Event == nil || data.Event.Message == nil || data.Event.Sender == nil {
		return nil
	}
	event := data.Event
	message := event.Message
	sender := event.Sender

	// Skip bot messages.
	if sender.SenderType != nil && *sender.SenderType == "bot" {
		return nil
	}

	// Deduplication.
	messageID := ""
	if message.MessageId != nil {
		messageID = *message.MessageId
	}
	if messageID != "" {
		if !f.markSeen(messageID) {
			return nil
		}
	}

	// Extract sender / chat IDs.
	senderID := "unknown"
	if sender.SenderId != nil && sender.SenderId.OpenId != nil {
		senderID = *sender.SenderId.OpenId
	}

	chatID := ""
	if message.ChatId != nil {
		chatID = *message.ChatId
	}

	chatType := ""
	if message.ChatType != nil {
		chatType = *message.ChatType
	}

	msgType := ""
	if message.MessageType != nil {
		msgType = *message.MessageType
	}

	// Add thumbsup reaction (non-blocking).
	if messageID != "" {
		go func() {
			if err := f.addReaction(context.Background(), messageID, "THUMBSUP"); err != nil {
				slog.Warn("feishu: add reaction failed", "error", err)
			}
		}()
	}

	// Parse content.
	rawContent := ""
	if message.Content != nil {
		rawContent = *message.Content
	}
	var contentJSON map[string]any
	if rawContent != "" {
		_ = json.Unmarshal([]byte(rawContent), &contentJSON)
	}

	text := f.extractContent(msgType, contentJSON)
	if text == "" {
		return nil
	}

	// Route: use chat_id for group, sender open_id for p2p.
	replyTo := chatID
	if chatType != "group" {
		replyTo = senderID
	}

	return f.HandleMessage(ctx, feishuChannelName, senderID, replyTo, text, map[string]any{
		"message_id": messageID,
		"chat_type":  chatType,
		"msg_type":   msgType,
	})
}

// markSeen adds msgID to the dedup cache. Returns true if the message is new,
// false if it was already seen.
func (f *FeishuChannel) markSeen(msgID string) bool {
	f.dedupMu.Lock()
	defer f.dedupMu.Unlock()
	if _, exists := f.dedupSet[msgID]; exists {
		return false
	}
	f.dedupSet[msgID] = struct{}{}
	f.dedupKeys = append(f.dedupKeys, msgID)
	// Evict oldest entries if over limit.
	for len(f.dedupKeys) > maxDedupSize {
		oldest := f.dedupKeys[0]
		f.dedupKeys = f.dedupKeys[1:]
		delete(f.dedupSet, oldest)
	}
	return true
}

// extractContent turns a parsed content JSON map into a plain-text string.
func (f *FeishuChannel) extractContent(msgType string, content map[string]any) string {
	switch msgType {
	case "text":
		if v, ok := content["text"].(string); ok {
			return v
		}
	case "post":
		return extractPostText(content)
	default:
		return fmt.Sprintf("[%s]", msgType)
	}
	return ""
}

// extractPostText extracts plain text from a Feishu post (rich text) message.
func extractPostText(content map[string]any) string {
	// Try localized keys first.
	for _, lang := range []string{"zh_cn", "en_us", "ja_jp"} {
		if lc, ok := content[lang].(map[string]any); ok {
			if t := extractPostLang(lc); t != "" {
				return t
			}
		}
	}
	// Direct format.
	return extractPostLang(content)
}

func extractPostLang(lc map[string]any) string {
	if lc == nil {
		return ""
	}
	var parts []string
	if title, ok := lc["title"].(string); ok && title != "" {
		parts = append(parts, title)
	}
	blocks, _ := lc["content"].([]any)
	for _, block := range blocks {
		row, ok := block.([]any)
		if !ok {
			continue
		}
		for _, elem := range row {
			el, ok := elem.(map[string]any)
			if !ok {
				continue
			}
			switch el["tag"] {
			case "text", "a":
				if t, ok := el["text"].(string); ok {
					parts = append(parts, t)
				}
			case "at":
				if name, ok := el["user_name"].(string); ok {
					parts = append(parts, "@"+name)
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// Sending helpers
// ---------------------------------------------------------------------------

// sendMessage sends a single message via the Feishu REST API.
func (f *FeishuChannel) sendMessage(ctx context.Context, receiveIDType, receiveID, msgType, content string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build()

	resp, err := f.client.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: send message: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: send message failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// addReaction adds an emoji reaction to the given message.
func (f *FeishuChannel) addReaction(ctx context.Context, messageID, emojiType string) error {
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
			Build()).
		Build()

	resp, err := f.client.Im.MessageReaction.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: add reaction: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu: add reaction failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Card building — converts markdown text to Feishu card elements
// ---------------------------------------------------------------------------

var (
	tableRE     = regexp.MustCompile(`(?m)((?:^[ \t]*\|.+\|[ \t]*\n)(?:^[ \t]*\|[-:\s|]+\|[ \t]*\n)(?:^[ \t]*\|.+\|[ \t]*\n?)+)`)
	headingRE   = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	codeBlockRE = regexp.MustCompile("(?s)(```.*?```)")
)

// buildCardElements converts a markdown string into a list of Feishu card
// elements (markdown divs, table elements, heading divs).
func (f *FeishuChannel) buildCardElements(content string) []map[string]any {
	var elements []map[string]any
	lastEnd := 0
	for _, m := range tableRE.FindAllStringIndex(content, -1) {
		before := content[lastEnd:m[0]]
		if strings.TrimSpace(before) != "" {
			elements = append(elements, f.splitHeadings(before)...)
		}
		tableText := content[m[0]:m[1]]
		if tableEl := parseMarkdownTable(tableText); tableEl != nil {
			elements = append(elements, tableEl)
		} else {
			elements = append(elements, map[string]any{"tag": "markdown", "content": tableText})
		}
		lastEnd = m[1]
	}
	remaining := content[lastEnd:]
	if strings.TrimSpace(remaining) != "" {
		elements = append(elements, f.splitHeadings(remaining)...)
	}
	if len(elements) == 0 {
		elements = []map[string]any{{"tag": "markdown", "content": content}}
	}
	return elements
}

// splitHeadings splits a text block by markdown headings, converting them to
// bold div elements and leaving other content as markdown elements.
func (f *FeishuChannel) splitHeadings(content string) []map[string]any {
	// Protect code blocks.
	protected := content
	var codeBlocks []string
	for _, m := range codeBlockRE.FindAllString(content, -1) {
		placeholder := fmt.Sprintf("\x00CODE%d\x00", len(codeBlocks))
		codeBlocks = append(codeBlocks, m)
		protected = strings.Replace(protected, m, placeholder, 1)
	}

	var elements []map[string]any
	lastEnd := 0
	for _, m := range headingRE.FindAllStringIndex(protected, -1) {
		before := strings.TrimSpace(protected[lastEnd:m[0]])
		if before != "" {
			elements = append(elements, map[string]any{"tag": "markdown", "content": before})
		}
		// The heading text is captured in the second group.
		full := protected[m[0]:m[1]]
		// Extract heading text after "### ".
		hashEnd := strings.IndexByte(full, ' ')
		var headText string
		if hashEnd >= 0 {
			headText = strings.TrimSpace(full[hashEnd+1:])
		} else {
			headText = full
		}
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "lark_md",
				"content": "**" + headText + "**",
			},
		})
		lastEnd = m[1]
	}
	remaining := strings.TrimSpace(protected[lastEnd:])
	if remaining != "" {
		elements = append(elements, map[string]any{"tag": "markdown", "content": remaining})
	}

	// Restore code blocks.
	for i, cb := range codeBlocks {
		placeholder := fmt.Sprintf("\x00CODE%d\x00", i)
		for _, el := range elements {
			if el["tag"] == "markdown" {
				if s, ok := el["content"].(string); ok {
					el["content"] = strings.ReplaceAll(s, placeholder, cb)
				}
			}
		}
	}

	if len(elements) == 0 {
		return []map[string]any{{"tag": "markdown", "content": content}}
	}
	return elements
}

// parseMarkdownTable converts a markdown table string to a Feishu table element.
func parseMarkdownTable(tableText string) map[string]any {
	lines := strings.Split(strings.TrimSpace(tableText), "\n")
	var nonEmpty []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, strings.TrimSpace(l))
		}
	}
	if len(nonEmpty) < 3 {
		return nil
	}
	splitRow := func(row string) []string {
		row = strings.Trim(row, "|")
		parts := strings.Split(row, "|")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		return parts
	}
	headers := splitRow(nonEmpty[0])
	var columns []map[string]any
	for i, h := range headers {
		columns = append(columns, map[string]any{
			"tag":          "column",
			"name":         fmt.Sprintf("c%d", i),
			"display_name": h,
			"width":        "auto",
		})
	}
	var rows []map[string]any
	for _, line := range nonEmpty[2:] {
		cells := splitRow(line)
		row := map[string]any{}
		for i := range headers {
			val := ""
			if i < len(cells) {
				val = cells[i]
			}
			row[fmt.Sprintf("c%d", i)] = val
		}
		rows = append(rows, row)
	}
	return map[string]any{
		"tag":       "table",
		"page_size": len(rows) + 1,
		"columns":   columns,
		"rows":      rows,
	}
}
