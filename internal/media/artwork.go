// Package media downloads and decodes visual assets used by the renderer.
package media

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

// ArtworkLoader fetches square album art from Apple's catalog CDN.
type ArtworkLoader struct {
	client *http.Client
	size   int
}

func NewArtworkLoader(size int) *ArtworkLoader {
	if size < 300 {
		size = 900
	}
	return &ArtworkLoader{client: &http.Client{Timeout: 20 * time.Second}, size: size}
}

func (l *ArtworkLoader) Load(ctx context.Context, track model.Track) (image.Image, error) {
	u := track.Artwork(l.size)
	if u == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ffmpeg-go-streamer/1.0")
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download artwork: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download artwork: HTTP %d", resp.StatusCode)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, fmt.Errorf("decode artwork: %w", err)
	}
	return img, nil
}
