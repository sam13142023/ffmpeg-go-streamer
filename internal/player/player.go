// Package player owns the shared playback queue and coordinates media
// preparation, rendering, and FFmpeg output.
package player

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"sync"
	"time"

	"github.com/sam1314/ffmpeg-go-streamer/internal/audio"
	"github.com/sam1314/ffmpeg-go-streamer/internal/lyrics"
	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
	"github.com/sam1314/ffmpeg-go-streamer/internal/render"
	"github.com/sam1314/ffmpeg-go-streamer/internal/stream"
)

type artworkLoader interface {
	Load(context.Context, model.Track) (image.Image, error)
}

type streamer interface {
	Play(context.Context, *render.Renderer, render.Frame, model.Audio, *stream.Controls) (stream.Result, error)
}

// State is the externally visible player state.
type State struct {
	Playing     bool
	Preparing   bool
	Paused      bool
	Now         model.Track
	Queue       []model.Track
	AudioSource string
	StartedAt   time.Time
	LastOutput  string
	LastError   string
}

// Player is a single shared radio-style queue. Telegram users all enqueue into
// the same stream, which prevents simultaneous FFmpeg writers from racing for
// one RTMP/HLS destination.
type Player struct {
	audio    audio.Resolver
	lyrics   lyrics.Provider
	artwork  artworkLoader
	renderer *render.Renderer
	streamer streamer

	mu        sync.Mutex
	queue     []model.Track
	state     State
	wake      chan struct{}
	cancel    context.CancelFunc
	controls  *stream.Controls
	last      model.Track // most recent track handed to the streamer, for Replay
	maxQueue  int
	assetWait time.Duration
}

func New(a audio.Resolver, l lyrics.Provider, art artworkLoader, renderer *render.Renderer, out streamer) *Player {
	return &Player{
		audio: a, lyrics: l, artwork: art, renderer: renderer, streamer: out,
		wake: make(chan struct{}, 1), maxQueue: 100, assetWait: 3 * time.Second,
	}
}

// Enqueue adds a track and returns its one-based queue position (zero means it
// became the next item while the player was idle).
func (p *Player) Enqueue(track model.Track) (int, error) {
	if track.AdamID == "" {
		return 0, fmt.Errorf("track has no catalog id")
	}
	p.mu.Lock()
	if len(p.queue) >= p.maxQueue {
		p.mu.Unlock()
		return 0, fmt.Errorf("queue is full")
	}
	p.queue = append(p.queue, track)
	position := len(p.queue)
	if !p.state.Playing && !p.state.Preparing {
		position = 0
	}
	p.state.Queue = append([]model.Track(nil), p.queue...)
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return position, nil
}

// Skip cancels the current preparation/playback. It returns false when idle.
func (p *Player) Skip() bool {
	p.mu.Lock()
	cancel := p.cancel
	p.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// Pause freezes the current track. Returns false when nothing is playing or
// it is already paused.
func (p *Player) Pause() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.state.Playing || p.controls == nil || !p.controls.Pause() {
		return false
	}
	p.state.Paused = true
	return true
}

// Resume continues a paused track. Returns false when nothing is paused.
func (p *Player) Resume() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.controls == nil || !p.controls.Resume() {
		return false
	}
	p.state.Paused = false
	return true
}

// Replay puts the current (or most recently started) track back at the front
// of the queue and restarts it from the beginning. Returns false when no
// track has been played yet.
func (p *Player) Replay() (model.Track, bool) {
	p.mu.Lock()
	track := p.state.Now
	if track.AdamID == "" {
		track = p.last
	}
	if track.AdamID == "" {
		p.mu.Unlock()
		return model.Track{}, false
	}
	p.queue = append([]model.Track{track}, p.queue...)
	p.state.Queue = append([]model.Track(nil), p.queue...)
	cancel := p.cancel
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return track, true
}

func (p *Player) Snapshot() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.state
	s.Queue = append([]model.Track(nil), p.queue...)
	return s
}

// Run processes queued tracks until ctx is canceled.
func (p *Player) Run(ctx context.Context) {
	for {
		track, ok := p.next(ctx)
		if !ok {
			return
		}
		p.playOne(ctx, track)
	}
}

func (p *Player) next(ctx context.Context) (model.Track, bool) {
	for {
		p.mu.Lock()
		if len(p.queue) > 0 {
			t := p.queue[0]
			p.queue = p.queue[1:]
			p.state.Queue = append([]model.Track(nil), p.queue...)
			p.mu.Unlock()
			return t, true
		}
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return model.Track{}, false
		case <-p.wake:
		}
	}
}

func (p *Player) playOne(parent context.Context, track model.Track) {
	ctx, cancel := context.WithCancel(parent)
	controls := stream.NewControls()
	p.mu.Lock()
	p.cancel = cancel
	p.controls = controls
	p.last = track
	p.state.Preparing = true
	p.state.Playing = false
	p.state.Paused = false
	p.state.Now = track
	p.state.LastError = ""
	p.state.AudioSource = ""
	p.mu.Unlock()

	type lyricResult struct {
		lyrics model.Lyrics
		err    error
	}
	type artResult struct {
		image image.Image
		err   error
	}
	lyricCh := make(chan lyricResult, 1)
	artCh := make(chan artResult, 1)
	go func() {
		l, err := p.lyrics.Get(ctx, track)
		lyricCh <- lyricResult{l, err}
	}()
	go func() {
		img, err := p.artwork.Load(ctx, track)
		artCh <- artResult{img, err}
	}()

	a, audioErr := p.audio.Resolve(ctx, track)
	if audioErr != nil {
		p.finish(cancel, fmt.Errorf("prepare audio: %w", audioErr), "")
		return
	}
	defer a.Close()
	// Lyrics and artwork enrich the video, but neither is allowed to hold the
	// audio queue hostage. Most lookups complete while audio is preparing; any
	// slow or broken provider gets only a short grace period after audio is ready.
	lr, ar := lyricResult{}, artResult{}
	gotLyrics, gotArtwork := false, false
	waitTimer := time.NewTimer(p.assetWait)
	for !gotLyrics || !gotArtwork {
		select {
		case lr = <-lyricCh:
			gotLyrics = true
		case ar = <-artCh:
			gotArtwork = true
		case <-waitTimer.C:
			if !gotLyrics {
				lr.err = fmt.Errorf("lyrics lookup exceeded %s; continuing without lyrics", p.assetWait)
			}
			if !gotArtwork {
				ar.err = fmt.Errorf("artwork lookup exceeded %s; continuing without artwork", p.assetWait)
			}
			gotLyrics, gotArtwork = true, true
		}
	}
	if !waitTimer.Stop() {
		select {
		case <-waitTimer.C:
		default:
		}
	}
	if lr.err != nil {
		slog.Warn("lyrics unavailable; continuing playback", "track", track.Label(), "error", lr.err)
	}
	if ar.err != nil {
		slog.Warn("artwork unavailable", "track", track.Label(), "error", ar.err)
	}

	p.mu.Lock()
	p.state.Preparing = false
	p.state.Playing = true
	p.state.StartedAt = time.Now()
	p.state.AudioSource = a.Source
	p.mu.Unlock()

	result, err := p.streamer.Play(ctx, p.renderer, render.Frame{
		Track: track, Artwork: ar.image, Lyrics: lr.lyrics,
		DurationMS: a.DurationMS, Playing: true,
	}, a, controls)
	p.finish(cancel, err, result.Path)
}

func (p *Player) finish(cancel context.CancelFunc, err error, output string) {
	cancel()
	p.mu.Lock()
	p.cancel = nil
	p.controls = nil
	p.state.Playing = false
	p.state.Preparing = false
	p.state.Paused = false
	p.state.Now = model.Track{}
	if output != "" {
		p.state.LastOutput = output
	}
	if err != nil && err != context.Canceled {
		p.state.LastError = err.Error()
		slog.Error("track failed", "error", err)
	}
	p.mu.Unlock()
}
