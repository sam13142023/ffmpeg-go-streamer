package telegram

import (
	"strings"
	"testing"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

func TestSearchMessageAndKeyboard(t *testing.T) {
	ss := &session{ID: "abc123", Query: "yellow", Tracks: []model.Track{
		{AdamID: "1122777203", Title: "Yellow", Artist: "Coldplay", Region: "us"},
		{AdamID: "2", Title: "YELLOW", Artist: "TREASURE", Region: "us"},
	}}
	text, keyboard := searchMessage(ss, 5)
	if !strings.Contains(text, "Search results for '<b>yellow</b>'") || !strings.Contains(text, "Yellow - Coldplay") {
		t.Fatalf("unexpected text %q", text)
	}
	if len(keyboard.InlineKeyboard) != 1 || len(keyboard.InlineKeyboard[0]) != 2 {
		t.Fatalf("unexpected keyboard %#v", keyboard)
	}
	if got := *keyboard.InlineKeyboard[0][0].CallbackData; got != "pick:abc123:0" {
		t.Fatalf("callback = %q", got)
	}
}

func TestSessionsAreOwnedByUser(t *testing.T) {
	store := newSessionStore()
	ss := store.create(10, 20, "song")
	got, ok := store.get(ss.ID)
	if !ok || got.ChatID != 10 || got.UserID != 20 {
		t.Fatalf("unexpected session %#v, ok=%v", got, ok)
	}
}
