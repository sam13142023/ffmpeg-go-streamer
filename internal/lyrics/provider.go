package lyrics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

// Provider fetches lyrics for a track. Implementations must not panic on
// missing lyrics; they return an empty model.Lyrics instead.
type Provider interface {
	Get(ctx context.Context, t model.Track) (model.Lyrics, error)
}

// FallbackProvider tries providers in order and returns the first non-empty
// result. Individual 404s/network errors are collected only if every provider
// fails; callers can then log one warning and continue playback without lyrics.
type FallbackProvider struct {
	Providers []Provider
}

func (p FallbackProvider) Get(ctx context.Context, track model.Track) (model.Lyrics, error) {
	var failures []error
	for _, provider := range p.Providers {
		if provider == nil {
			continue
		}
		result, err := provider.Get(ctx, track)
		if err == nil {
			return result, nil
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%T: %w", provider, err))
		}
	}
	if len(failures) == 0 {
		return model.Lyrics{}, nil
	}
	return model.Lyrics{}, errors.Join(failures...)
}

// TTMLSource is the minimal wrapper capability the lyrics provider needs. The
// wrapper.Client satisfies this interface.
type TTMLSource interface {
	LyricsTTML(ctx context.Context, adamID, region, language string) (string, error)
}

// WrapperProvider fetches synced TTML lyrics from a wrapper-manager instance.
type WrapperProvider struct {
	src      TTMLSource
	language string
}

// NewWrapperProvider builds a provider backed by a TTMLSource (wrapper client).
func NewWrapperProvider(src TTMLSource, language string) *WrapperProvider {
	return &WrapperProvider{src: src, language: language}
}

// Get fetches and parses lyrics for the track. On any error it returns empty
// (unsynced) lyrics with the error, so the caller can still render a frame.
func (p *WrapperProvider) Get(ctx context.Context, t model.Track) (model.Lyrics, error) {
	wrapperCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ttml, err := p.src.LyricsTTML(wrapperCtx, t.AdamID, t.Region, p.language)
	if err != nil {
		return model.Lyrics{}, err
	}
	parsed, err := ParseTTML(ttml)
	if err == nil && parsed.Empty() {
		return model.Lyrics{}, ErrNotFound
	}
	return parsed, err
}

// EmptyProvider always returns empty lyrics. Used when no wrapper is configured.
type EmptyProvider struct{}

// Get returns empty lyrics and no error.
func (EmptyProvider) Get(context.Context, model.Track) (model.Lyrics, error) {
	return model.Lyrics{}, nil
}
