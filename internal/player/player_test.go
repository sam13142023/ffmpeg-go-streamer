package player

import (
	"context"
	"image"
	"testing"
	"time"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
	"github.com/sam1314/ffmpeg-go-streamer/internal/render"
	"github.com/sam1314/ffmpeg-go-streamer/internal/stream"
)

type instantAudio struct{}

func (instantAudio) Resolve(context.Context, model.Track) (model.Audio, error) {
	return model.NewAudio("fake-audio", 1000, "test", nil), nil
}

type blockingLyrics struct{}

func (blockingLyrics) Get(ctx context.Context, _ model.Track) (model.Lyrics, error) {
	<-ctx.Done()
	return model.Lyrics{}, ctx.Err()
}

type instantArtwork struct{}

func (instantArtwork) Load(context.Context, model.Track) (image.Image, error) { return nil, nil }

type recordingStreamer struct{ called chan struct{} }

func (s recordingStreamer) Play(context.Context, *render.Renderer, render.Frame, model.Audio, *stream.Controls) (stream.Result, error) {
	close(s.called)
	return stream.Result{}, nil
}

type blockingStreamer struct{ started chan struct{} }

func (s blockingStreamer) Play(ctx context.Context, _ *render.Renderer, _ render.Frame, _ model.Audio, _ *stream.Controls) (stream.Result, error) {
	s.started <- struct{}{}
	<-ctx.Done()
	return stream.Result{}, ctx.Err()
}

func TestLyricsFailureCannotBlockPlayback(t *testing.T) {
	r, err := render.New(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	called := make(chan struct{})
	p := New(instantAudio{}, blockingLyrics{}, instantArtwork{}, r, recordingStreamer{called: called})
	p.assetWait = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	if _, err := p.Enqueue(model.Track{AdamID: "1", Title: "Yellow", Artist: "Coldplay"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("streamer was blocked by lyrics lookup")
	}
}

func TestPauseResumeReplay(t *testing.T) {
	r, err := render.New(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	p := New(instantAudio{}, blockingLyrics{}, instantArtwork{}, r, blockingStreamer{started: started})
	p.assetWait = 20 * time.Millisecond

	if p.Pause() {
		t.Fatal("Pause should fail while idle")
	}
	if _, ok := p.Replay(); ok {
		t.Fatal("Replay should fail before anything played")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	track := model.Track{AdamID: "1", Title: "Yellow", Artist: "Coldplay"}
	if _, err := p.Enqueue(track); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("playback never started")
	}

	if !p.Pause() {
		t.Fatal("Pause should succeed while playing")
	}
	if p.Pause() {
		t.Fatal("second Pause should report already paused")
	}
	if !p.Snapshot().Paused {
		t.Fatal("state should be paused")
	}
	if !p.Resume() {
		t.Fatal("Resume should succeed while paused")
	}
	if p.Resume() {
		t.Fatal("second Resume should report not paused")
	}

	got, ok := p.Replay()
	if !ok || got.AdamID != track.AdamID {
		t.Fatalf("Replay = %#v, %v", got, ok)
	}
	// Replay cancels the current playback and restarts the same track.
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("replayed track never started")
	}
}
