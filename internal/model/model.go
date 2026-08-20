// Package model holds the shared domain types passed between the search,
// audio, lyrics, render, stream and bot layers.
package model

import (
	"fmt"
	"strings"
	"time"
)

// Track is a single Apple Music song. AdamID is the Apple Music catalog id
// (identical to the iTunes Search API's trackId) and is the key used by every
// wrapper-manager RPC.
type Track struct {
	AdamID     string
	Title      string
	Artist     string
	Album      string
	ArtworkURL string // iTunes template URL, ends in ".../{w}x{h}bb.jpg"
	PreviewURL string // 30s AAC preview, publicly downloadable, no auth
	DurationMS int    // full-track duration in ms (from iTunes metadata)
	Region     string // storefront / country code, e.g. "us"
}

// Label returns a "Title - Artist" one-liner used in search result listings.
func (t Track) Label() string {
	if t.Artist == "" {
		return t.Title
	}
	return fmt.Sprintf("%s - %s", t.Title, t.Artist)
}

// Artwork returns a concrete artwork URL at the requested square size by
// substituting the iTunes {w}x{h} template. Falls back to the raw URL when the
// template markers are absent.
func (t Track) Artwork(size int) string {
	if t.ArtworkURL == "" {
		return ""
	}
	if strings.Contains(t.ArtworkURL, "{w}") || strings.Contains(t.ArtworkURL, "{h}") {
		r := strings.NewReplacer("{w}", itoa(size), "{h}", itoa(size))
		return r.Replace(t.ArtworkURL)
	}
	return t.ArtworkURL
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// LyricWord is a single timed syllable/word inside a lyric line (Apple's
// syllable TTML carries per-word timing used for karaoke-style highlighting).
type LyricWord struct {
	Text    string
	StartMS int
	EndMS   int
}

// LyricLine is one timed line of lyrics.
type LyricLine struct {
	StartMS int
	EndMS   int
	Text    string
	Words   []LyricWord // optional, empty when only line-level timing exists
}

// Lyrics is the parsed, ordered set of timed lines for a track.
type Lyrics struct {
	Lines    []LyricLine
	Synced   bool   // true when the lines carry real timing
	Language string // BCP-47 tag when known
}

// ActiveLineAt returns the index of the line that should be highlighted at the
// given playback position, or -1 when nothing is active yet. Lines are assumed
// ordered by StartMS.
func (l Lyrics) ActiveLineAt(posMS int) int {
	if !l.Synced {
		return -1
	}
	active := -1
	for i, ln := range l.Lines {
		if posMS >= ln.StartMS {
			active = i
		} else {
			break
		}
	}
	return active
}

// Empty reports whether there are no usable lyric lines.
func (l Lyrics) Empty() bool { return len(l.Lines) == 0 }

// Audio is a fetched, locally-playable audio file ready to feed to ffmpeg.
type Audio struct {
	Path       string // local file path
	DurationMS int    // measured/known duration
	Source     string // "preview" | "wrapper" for reporting
	cleanup    func()
}

// NewAudio builds an Audio value with an optional cleanup closure.
func NewAudio(path string, durationMS int, source string, cleanup func()) Audio {
	return Audio{Path: path, DurationMS: durationMS, Source: source, cleanup: cleanup}
}

// Close removes any temporary files backing this audio.
func (a Audio) Close() {
	if a.cleanup != nil {
		a.cleanup()
	}
}

// Duration returns the duration as a time.Duration.
func (a Audio) Duration() time.Duration { return time.Duration(a.DurationMS) * time.Millisecond }
