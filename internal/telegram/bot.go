// Package telegram implements the command and inline-keyboard UX shown in the
// reference Telegram screenshot.
package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/sam1314/ffmpeg-go-streamer/internal/applemusic"
	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
	"github.com/sam1314/ffmpeg-go-streamer/internal/player"
)

type session struct {
	ID      string
	ChatID  int64
	UserID  int64
	Query   string
	Offset  int
	Tracks  []model.Track
	Touched time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func newSessionStore() *sessionStore { return &sessionStore{sessions: map[string]*session{}} }

func (s *sessionStore) create(chatID, userID int64, query string) *session {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, old := range s.sessions {
		if now.Sub(old.Touched) > 30*time.Minute {
			delete(s.sessions, key)
		}
	}
	ss := &session{ID: id, ChatID: chatID, UserID: userID, Query: query, Touched: now}
	s.sessions[id] = ss
	return ss
}

func (s *sessionStore) get(id string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.sessions[id]
	if !ok || time.Since(ss.Touched) > 30*time.Minute {
		delete(s.sessions, id)
		return nil, false
	}
	ss.Touched = time.Now()
	copy := *ss
	copy.Tracks = append([]model.Track(nil), ss.Tracks...)
	return &copy, true
}

func (s *sessionStore) update(ss *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss.Touched = time.Now()
	s.sessions[ss.ID] = ss
}

// Bot owns Telegram polling and delegates selected tracks to the shared player.
type Bot struct {
	api        *tgbotapi.BotAPI
	catalog    *applemusic.Client
	player     *player.Player
	maxResults int
	sessions   *sessionStore
}

func New(token string, catalog *applemusic.Client, p *player.Player, maxResults int) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("connect Telegram bot: %w", err)
	}
	return &Bot{api: api, catalog: catalog, player: p, maxResults: maxResults, sessions: newSessionStore()}, nil
}

// Run receives long-poll updates until ctx is canceled.
func (b *Bot) Run(ctx context.Context) error {
	slog.Info("telegram bot connected", "username", b.api.Self.UserName)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30
	updates := b.api.GetUpdatesChan(u)
	defer b.api.StopReceivingUpdates()
	for {
		select {
		case <-ctx.Done():
			return nil
		case update, ok := <-updates:
			if !ok {
				return nil
			}
			if update.CallbackQuery != nil {
				b.handleCallback(ctx, update.CallbackQuery)
			} else if update.Message != nil && update.Message.IsCommand() {
				b.handleCommand(ctx, update.Message)
			}
		}
	}
}

func (b *Bot) handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	switch strings.ToLower(msg.Command()) {
	case "start", "help":
		b.send(msg.Chat.ID, helpText())
	case "search":
		query := strings.TrimSpace(msg.CommandArguments())
		if query == "" {
			b.send(msg.Chat.ID, "Usage: <code>/search song or artist</code>")
			return
		}
		ss := b.sessions.create(msg.Chat.ID, msg.From.ID, query)
		b.searchAndSend(ctx, ss, msg.MessageID)
	case "status", "nowplaying":
		b.sendStatus(msg.Chat.ID)
	case "queue":
		b.send(msg.Chat.ID, formatQueue(b.player.Snapshot()))
	case "skip", "next":
		if b.player.Skip() {
			b.send(msg.Chat.ID, "⏭ Skipping the current track.")
		} else {
			b.send(msg.Chat.ID, "Nothing is playing.")
		}
	case "pause":
		if b.player.Pause() {
			b.send(msg.Chat.ID, "⏸ Paused. Use /resume to continue.")
		} else {
			b.send(msg.Chat.ID, "Nothing to pause.")
		}
	case "resume", "play":
		if b.player.Resume() {
			b.send(msg.Chat.ID, "▶️ Resumed.")
		} else {
			b.send(msg.Chat.ID, "Nothing is paused.")
		}
	case "replay":
		if track, ok := b.player.Replay(); ok {
			b.send(msg.Chat.ID, "🔁 Replaying <b>"+html.EscapeString(track.Label())+"</b> from the start.")
		} else {
			b.send(msg.Chat.ID, "Nothing to replay yet.")
		}
	default:
		b.send(msg.Chat.ID, "Unknown command. Use /help to see the available commands.")
	}
}

func (b *Bot) handleCallback(ctx context.Context, q *tgbotapi.CallbackQuery) {
	parts := strings.Split(q.Data, ":")
	if q.Message == nil || q.From == nil {
		b.answer(q.ID, "This button is no longer valid.", true)
		return
	}
	// Transport controls act on the shared player and need no search session.
	if len(parts) == 2 && parts[0] == "ctl" {
		b.handleControl(q, parts[1])
		return
	}
	if len(parts) != 3 {
		b.answer(q.ID, "This button is no longer valid.", true)
		return
	}
	ss, ok := b.sessions.get(parts[1])
	if !ok || ss.ChatID != q.Message.Chat.ID || ss.UserID != q.From.ID {
		b.answer(q.ID, "Please run /search again.", true)
		return
	}

	switch parts[0] {
	case "pick":
		i, err := strconv.Atoi(parts[2])
		if err != nil || i < 0 || i >= len(ss.Tracks) {
			b.answer(q.ID, "That result expired.", true)
			return
		}
		track := ss.Tracks[i]
		position, err := b.player.Enqueue(track)
		if err != nil {
			b.answer(q.ID, err.Error(), true)
			return
		}
		text := "▶️ Preparing " + track.Label()
		if position > 0 {
			text = fmt.Sprintf("➕ Added %s at queue position %d", track.Label(), position)
		}
		b.answer(q.ID, text, false)
		edit := tgbotapi.NewEditMessageText(q.Message.Chat.ID, q.Message.MessageID,
			fmt.Sprintf("🎵 <b>%s</b>\nSelected from results for <b>%s</b>.",
				html.EscapeString(track.Label()), html.EscapeString(ss.Query)))
		edit.ParseMode = "HTML"
		_, _ = b.api.Send(edit)
	case "page":
		offset, err := strconv.Atoi(parts[2])
		if err != nil || offset < 0 || offset > 195 {
			b.answer(q.ID, "Invalid page.", true)
			return
		}
		b.answer(q.ID, "Searching…", false)
		ss.Offset = offset
		b.searchAndEdit(ctx, ss, q.Message.MessageID)
	default:
		b.answer(q.ID, "Unknown action.", true)
	}
}

func (b *Bot) handleControl(q *tgbotapi.CallbackQuery, action string) {
	var text string
	switch action {
	case "pause":
		if b.player.Pause() {
			text = "⏸ Paused."
		} else {
			text = "Nothing to pause."
		}
	case "resume":
		if b.player.Resume() {
			text = "▶️ Resumed."
		} else {
			text = "Nothing is paused."
		}
	case "next":
		if b.player.Skip() {
			text = "⏭ Skipping the current track."
		} else {
			text = "Nothing is playing."
		}
	case "replay":
		if track, ok := b.player.Replay(); ok {
			text = "🔁 Replaying " + track.Label()
		} else {
			text = "Nothing to replay yet."
		}
	default:
		b.answer(q.ID, "Unknown action.", true)
		return
	}
	b.answer(q.ID, text, false)
	// Refresh the status message the buttons live on. Skip/replay finish
	// asynchronously, so this snapshot may briefly show the outgoing track.
	s := b.player.Snapshot()
	edit := tgbotapi.NewEditMessageTextAndMarkup(q.Message.Chat.ID, q.Message.MessageID, formatStatus(s), controlKeyboard(s))
	edit.ParseMode = "HTML"
	_, _ = b.api.Send(edit)
}

func (b *Bot) sendStatus(chatID int64) {
	s := b.player.Snapshot()
	m := tgbotapi.NewMessage(chatID, formatStatus(s))
	m.ParseMode = "HTML"
	m.DisableWebPagePreview = true
	m.ReplyMarkup = controlKeyboard(s)
	if _, err := b.api.Send(m); err != nil {
		slog.Error("send status message", "error", err)
	}
}

func controlKeyboard(s player.State) tgbotapi.InlineKeyboardMarkup {
	toggle := tgbotapi.NewInlineKeyboardButtonData("⏸ Pause", "ctl:pause")
	if s.Paused {
		toggle = tgbotapi.NewInlineKeyboardButtonData("▶️ Resume", "ctl:resume")
	}
	return tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(
		toggle,
		tgbotapi.NewInlineKeyboardButtonData("⏭ Next", "ctl:next"),
		tgbotapi.NewInlineKeyboardButtonData("🔁 Replay", "ctl:replay"),
	))
}

func (b *Bot) searchAndSend(ctx context.Context, ss *session, replyTo int) {
	tracks, err := b.catalog.Search(ctx, ss.Query, b.maxResults, ss.Offset)
	if err != nil {
		b.send(ss.ChatID, "Search failed: "+html.EscapeString(err.Error()))
		return
	}
	if len(tracks) == 0 {
		b.send(ss.ChatID, "No songs found for <b>"+html.EscapeString(ss.Query)+"</b>.")
		return
	}
	ss.Tracks = tracks
	b.sessions.update(ss)
	text, keyboard := searchMessage(ss, b.maxResults)
	m := tgbotapi.NewMessage(ss.ChatID, text)
	m.ParseMode = "HTML"
	m.DisableWebPagePreview = true
	m.ReplyMarkup = keyboard
	m.ReplyToMessageID = replyTo
	if _, err := b.api.Send(m); err != nil {
		slog.Error("send search results", "error", err)
	}
}

func (b *Bot) searchAndEdit(ctx context.Context, ss *session, messageID int) {
	tracks, err := b.catalog.Search(ctx, ss.Query, b.maxResults, ss.Offset)
	if err != nil || len(tracks) == 0 {
		b.send(ss.ChatID, "No more results.")
		return
	}
	ss.Tracks = tracks
	b.sessions.update(ss)
	text, keyboard := searchMessage(ss, b.maxResults)
	edit := tgbotapi.NewEditMessageTextAndMarkup(ss.ChatID, messageID, text, keyboard)
	edit.ParseMode = "HTML"
	edit.DisableWebPagePreview = true
	if _, err := b.api.Send(edit); err != nil {
		slog.Error("edit search results", "error", err)
	}
}

func searchMessage(ss *session, pageSize int) (string, tgbotapi.InlineKeyboardMarkup) {
	var text strings.Builder
	fmt.Fprintf(&text, "Search results for '<b>%s</b>':", html.EscapeString(ss.Query))
	buttons := make([]tgbotapi.InlineKeyboardButton, 0, len(ss.Tracks))
	for i, track := range ss.Tracks {
		link := fmt.Sprintf("https://music.apple.com/%s/song/%s", track.Region, track.AdamID)
		fmt.Fprintf(&text, "\n%d. <a href=\"%s\">%s</a>", ss.Offset+i+1, link, html.EscapeString(track.Label()))
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(strconv.Itoa(i+1), fmt.Sprintf("pick:%s:%d", ss.ID, i)))
	}
	rows := [][]tgbotapi.InlineKeyboardButton{tgbotapi.NewInlineKeyboardRow(buttons...)}
	var nav []tgbotapi.InlineKeyboardButton
	if ss.Offset > 0 {
		prev := ss.Offset - pageSize
		if prev < 0 {
			prev = 0
		}
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("‹", fmt.Sprintf("page:%s:%d", ss.ID, prev)))
	}
	if len(ss.Tracks) == pageSize && ss.Offset+pageSize < 200 {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("›", fmt.Sprintf("page:%s:%d", ss.ID, ss.Offset+pageSize)))
	}
	if len(nav) > 0 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(nav...))
	}
	return text.String(), tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func formatStatus(s player.State) string {
	if s.Playing || s.Preparing {
		verb := "Playing"
		if s.Preparing {
			verb = "Preparing"
		}
		if s.Paused {
			verb = "Paused"
		}
		return fmt.Sprintf("🎧 <b>%s</b>\n%s\nAudio: %s\nQueued: %d",
			verb, html.EscapeString(s.Now.Label()), html.EscapeString(valueOr(s.AudioSource, "pending")), len(s.Queue))
	}
	text := "⏹ <b>Not playing</b>"
	if s.LastError != "" {
		text += "\nLast error: <code>" + html.EscapeString(s.LastError) + "</code>"
	}
	return text
}

func formatQueue(s player.State) string {
	if len(s.Queue) == 0 {
		return "The queue is empty. Use <code>/search song or artist</code>."
	}
	var out strings.Builder
	out.WriteString("<b>Up next</b>")
	for i, track := range s.Queue {
		if i == 10 {
			fmt.Fprintf(&out, "\n…and %d more", len(s.Queue)-i)
			break
		}
		fmt.Fprintf(&out, "\n%d. %s", i+1, html.EscapeString(track.Label()))
	}
	return out.String()
}

func helpText() string {
	return "<b>Live Music Video</b>\n\n" +
		"<code>/search yellow</code> — find music\n" +
		"/status — current track and playback controls\n" +
		"/queue — upcoming tracks\n" +
		"/pause — pause the current track\n" +
		"/resume — continue after a pause\n" +
		"/next — skip to the next track (/skip)\n" +
		"/replay — restart the current track\n" +
		"/help — show this message"
}

func (b *Bot) send(chatID int64, text string) {
	m := tgbotapi.NewMessage(chatID, text)
	m.ParseMode = "HTML"
	m.DisableWebPagePreview = true
	if _, err := b.api.Send(m); err != nil {
		slog.Error("send Telegram message", "error", err)
	}
}

func (b *Bot) answer(id, text string, alert bool) {
	cfg := tgbotapi.NewCallback(id, text)
	cfg.ShowAlert = alert
	_, _ = b.api.Request(cfg)
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
