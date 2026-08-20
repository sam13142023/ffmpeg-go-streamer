// Package stream turns rendered Go image frames plus audio into MP4, HLS, or
// an RTMP live stream using FFmpeg.
package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sam1314/ffmpeg-go-streamer/internal/config"
	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
	"github.com/sam1314/ffmpeg-go-streamer/internal/render"
)

// Result describes the stream artifact created for a track. RTMP mode has an
// empty Path because the destination is remote.
type Result struct {
	Path     string
	Duration time.Duration
}

// FFmpeg streams frames from Renderer in real time.
type FFmpeg struct {
	binary    string
	mode      config.StreamMode
	rtmpURL   string
	outputDir string
	fps       int
}

func NewFFmpeg(binary string, mode config.StreamMode, rtmpURL, outputDir string, fps int) *FFmpeg {
	return &FFmpeg{binary: binary, mode: mode, rtmpURL: rtmpURL, outputDir: outputDir, fps: fps}
}

// WritePNG saves a renderer frame, primarily used for the idle/preview page.
func WritePNG(path string, frame image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, frame)
}

// Play starts FFmpeg, feeds it one RGBA frame per tick, and returns after the
// audio duration or cancellation. Cancellation immediately stops FFmpeg.
// While controls reports paused, no frames are fed: the muxer stalls waiting
// for video, which also halts the audio it interleaves, so live (RTMP/HLS)
// output genuinely freezes. Paused time is excluded from the playback clock.
func (s *FFmpeg) Play(ctx context.Context, renderer *render.Renderer, frame render.Frame, audio model.Audio, controls *Controls) (Result, error) {
	if audio.Path == "" {
		return Result{}, errors.New("empty audio path")
	}
	if s.fps <= 0 {
		return Result{}, errors.New("invalid frame rate")
	}
	if err := os.MkdirAll(s.outputDir, 0o755); err != nil {
		return Result{}, err
	}

	w, h := renderer.Size()
	output, outArgs, err := s.outputArgs(frame.Track)
	if err != nil {
		return Result{}, err
	}
	args := []string{
		"-hide_banner", "-loglevel", "warning", "-y",
		"-thread_queue_size", "512",
		"-f", "rawvideo", "-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", w, h),
		"-framerate", fmt.Sprintf("%d", s.fps), "-i", "pipe:0",
		"-i", audio.Path,
		"-map", "0:v:0", "-map", "1:a:0",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage",
		"-pix_fmt", "yuv420p", "-g", fmt.Sprintf("%d", s.fps*2),
		"-c:a", "aac", "-b:a", "192k", "-ar", "48000",
	}
	args = append(args, outArgs...)

	cmd := exec.CommandContext(ctx, s.binary, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start ffmpeg: %w", err)
	}

	duration := audio.Duration()
	if duration <= 0 {
		duration = time.Duration(frame.DurationMS) * time.Millisecond
	}
	if duration <= 0 {
		duration = 30 * time.Second
	}
	frame.DurationMS = int(duration / time.Millisecond)
	frame.Playing = true
	started := time.Now()
	interval := time.Second / time.Duration(s.fps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	writeFrame := func(position time.Duration) error {
		frame.PositionMS = int(position / time.Millisecond)
		img := renderer.Render(frame)
		_, err := writeAll(stdin, img.Pix)
		return err
	}

	var pausedFor time.Duration
	lastTick := started
	playErr := writeFrame(0)
	for playErr == nil {
		select {
		case <-ctx.Done():
			playErr = ctx.Err()
		case now := <-ticker.C:
			if controls.Paused() {
				pausedFor += now.Sub(lastTick)
				lastTick = now
				continue
			}
			lastTick = now
			pos := now.Sub(started) - pausedFor
			if pos >= duration {
				playErr = io.EOF
				break
			}
			playErr = writeFrame(pos)
		}
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 1200 {
			msg = msg[len(msg)-1200:]
		}
		return Result{}, fmt.Errorf("ffmpeg: %w: %s", waitErr, msg)
	}
	return Result{Path: output, Duration: time.Since(started) - pausedFor}, nil
}

func (s *FFmpeg) outputArgs(track model.Track) (string, []string, error) {
	switch s.mode {
	case config.StreamFile:
		name := fmt.Sprintf("%s-%d.mp4", safeName(track.Label()), time.Now().Unix())
		path := filepath.Join(s.outputDir, name)
		return path, []string{"-movflags", "+faststart", "-shortest", path}, nil
	case config.StreamHLS:
		dir := filepath.Join(s.outputDir, "hls")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", nil, err
		}
		path := filepath.Join(dir, "stream.m3u8")
		segments := filepath.Join(dir, "segment-%06d.ts")
		return path, []string{
			"-f", "hls", "-hls_time", "2", "-hls_list_size", "8",
			"-hls_flags", "delete_segments+independent_segments",
			"-hls_segment_filename", segments, path,
		}, nil
	case config.StreamRTMP:
		if s.rtmpURL == "" {
			return "", nil, errors.New("empty RTMP URL")
		}
		return "", []string{"-f", "flv", s.rtmpURL}, nil
	default:
		return "", nil, fmt.Errorf("unsupported stream mode %q", s.mode)
	}
}

func writeAll(w io.Writer, p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n, err := w.Write(p)
		total += n
		p = p[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func safeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
		if b.Len() >= 60 {
			break
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "track"
	}
	return name
}
