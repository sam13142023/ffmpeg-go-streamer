package lyrics

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

func TestLRCLibFallsBackFromExactToSearch(t *testing.T) {
	var requests int
	p := NewLRCLibProvider("https://lyrics.test")
	p.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if !strings.Contains(r.Header.Get("User-Agent"), "ffmpeg-go-streamer") {
			t.Error("missing identifying User-Agent")
		}
		status := http.StatusOK
		body := ""
		switch r.URL.Path {
		case "/api/get":
			status = http.StatusNotFound
		case "/api/search":
			body = `[{"trackName":"Yellow","artistName":"Coldplay","albumName":"Parachutes","duration":267,"syncedLyrics":"[00:01.00]Look at the stars\n[00:03.00]Look how they shine"}]`
		default:
			status = http.StatusNotFound
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
	got, err := p.Get(context.Background(), model.Track{
		AdamID: "1122777203", Title: "Yellow", Artist: "Coldplay", Album: "Parachutes", DurationMS: 267000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Synced || len(got.Lines) != 2 {
		t.Fatalf("unexpected result %#v", got)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	// The second lookup is served from the in-memory cache.
	if _, err := p.Get(context.Background(), model.Track{
		AdamID: "1122777203", Title: "Yellow", Artist: "Coldplay", Album: "Parachutes", DurationMS: 267000,
	}); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("cache missed; requests = %d", requests)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type testProvider struct {
	lyrics model.Lyrics
	err    error
}

func (p testProvider) Get(context.Context, model.Track) (model.Lyrics, error) {
	return p.lyrics, p.err
}

func TestFallbackProviderContinuesAfterFailure(t *testing.T) {
	want := model.Lyrics{Synced: true, Lines: []model.LyricLine{{StartMS: 100, Text: "fallback"}}}
	p := FallbackProvider{Providers: []Provider{
		testProvider{err: ErrNotFound},
		testProvider{lyrics: want},
	}}
	got, err := p.Get(context.Background(), model.Track{})
	if err != nil || len(got.Lines) != 1 || got.Lines[0].Text != "fallback" {
		t.Fatalf("got %#v, err=%v", got, err)
	}
}
