// Package audio resolves catalog tracks into local files FFmpeg can play.
package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

// Resolver prepares a local audio file for a track. Callers own and must close
// the returned Audio value.
type Resolver interface {
	Resolve(context.Context, model.Track) (model.Audio, error)
}

// M3U8Source is the wrapper-manager capability used by WrapperResolver.
type M3U8Source interface {
	M3U8(context.Context, string) (string, error)
}

// PreviewResolver downloads Apple's public 30-second catalog preview. It needs
// no credentials and makes the complete app testable before wrapper-manager is
// configured.
type PreviewResolver struct {
	client  *http.Client
	workDir string
	ffprobe string
}

func NewPreviewResolver(workDir, ffprobe string) *PreviewResolver {
	return &PreviewResolver{
		client:  &http.Client{Timeout: 45 * time.Second},
		workDir: workDir,
		ffprobe: ffprobe,
	}
}

func (r *PreviewResolver) Resolve(ctx context.Context, track model.Track) (model.Audio, error) {
	if strings.TrimSpace(track.PreviewURL) == "" {
		return model.Audio{}, errors.New("track has no public preview")
	}
	if err := os.MkdirAll(r.workDir, 0o755); err != nil {
		return model.Audio{}, err
	}
	f, err := os.CreateTemp(r.workDir, "preview-*.m4a")
	if err != nil {
		return model.Audio{}, err
	}
	path := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, track.PreviewURL, nil)
	if err != nil {
		return model.Audio{}, err
	}
	req.Header.Set("User-Agent", "ffmpeg-go-streamer/1.0")
	resp, err := r.client.Do(req)
	if err != nil {
		return model.Audio{}, fmt.Errorf("download preview: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return model.Audio{}, fmt.Errorf("download preview: HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 64<<20)); err != nil {
		return model.Audio{}, fmt.Errorf("save preview: %w", err)
	}
	if err := f.Close(); err != nil {
		return model.Audio{}, err
	}
	duration := probeDuration(ctx, r.ffprobe, path)
	if duration <= 0 {
		duration = 30_000
	}
	ok = true
	return model.NewAudio(path, duration, "preview", func() { _ = os.Remove(path) }), nil
}

// WrapperResolver asks wrapper-manager for the track's enhanced-HLS asset and
// lets FFmpeg remux/decode it to a local AAC file. Some wrapper deployments
// expose a directly playable asset; FairPlay-encrypted deployments will reject
// this path, allowing FallbackResolver to use the public preview instead.
// Synced lyrics still come from wrapper-manager in either case.
type WrapperResolver struct {
	source  M3U8Source
	workDir string
	ffmpeg  string
	ffprobe string
}

func NewWrapperResolver(source M3U8Source, workDir, ffmpeg, ffprobe string) *WrapperResolver {
	return &WrapperResolver{source: source, workDir: workDir, ffmpeg: ffmpeg, ffprobe: ffprobe}
}

func (r *WrapperResolver) Resolve(ctx context.Context, track model.Track) (model.Audio, error) {
	wrapperCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	asset, err := r.source.M3U8(wrapperCtx, track.AdamID)
	if err != nil {
		return model.Audio{}, fmt.Errorf("wrapper playlist: %w", err)
	}
	asset = strings.TrimSpace(asset)
	if asset == "" {
		return model.Audio{}, errors.New("wrapper returned an empty playlist")
	}
	if err := os.MkdirAll(r.workDir, 0o755); err != nil {
		return model.Audio{}, err
	}

	input := asset
	var playlistPath string
	if strings.HasPrefix(asset, "#EXTM3U") {
		f, err := os.CreateTemp(r.workDir, "wrapper-*.m3u8")
		if err != nil {
			return model.Audio{}, err
		}
		playlistPath = f.Name()
		if _, err := f.WriteString(asset); err != nil {
			_ = f.Close()
			_ = os.Remove(playlistPath)
			return model.Audio{}, err
		}
		_ = f.Close()
		defer os.Remove(playlistPath)
		input = playlistPath
	}

	out, err := os.CreateTemp(r.workDir, "wrapper-*.m4a")
	if err != nil {
		return model.Audio{}, err
	}
	outPath := out.Name()
	_ = out.Close()
	_ = os.Remove(outPath) // FFmpeg creates it itself.

	cmd := exec.CommandContext(ctx, r.ffmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-protocol_whitelist", "file,http,https,tcp,tls,crypto",
		"-i", input, "-vn", "-c:a", "aac", "-b:a", "256k",
		"-movflags", "+faststart", outPath,
	)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(outPath)
		msg := strings.TrimSpace(string(combined))
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return model.Audio{}, fmt.Errorf("decode wrapper asset: %w: %s", err, msg)
	}
	duration := probeDuration(ctx, r.ffprobe, outPath)
	if duration <= 0 {
		duration = track.DurationMS
	}
	return model.NewAudio(outPath, duration, "wrapper", func() { _ = os.Remove(outPath) }), nil
}

// FallbackResolver tries Primary and falls back to Secondary on any error.
// The first error is retained when both fail.
type FallbackResolver struct {
	Primary, Secondary Resolver
}

func (r FallbackResolver) Resolve(ctx context.Context, track model.Track) (model.Audio, error) {
	a, first := r.Primary.Resolve(ctx, track)
	if first == nil {
		return a, nil
	}
	a, second := r.Secondary.Resolve(ctx, track)
	if second != nil {
		return model.Audio{}, fmt.Errorf("full audio failed (%v); preview failed: %w", first, second)
	}
	return a, nil
}

func probeDuration(ctx context.Context, binary, path string) int {
	if strings.TrimSpace(binary) == "" {
		return 0
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, binary,
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", filepath.Clean(path),
	).Output()
	if err != nil {
		return 0
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return int(seconds*1000 + 0.5)
}
