// Package config loads runtime configuration from environment variables (and an
// optional .env file) with sensible zero-credential defaults so the app can run
// end-to-end in preview mode out of the box.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// StreamMode selects where the rendered video is sent.
type StreamMode string

const (
	// StreamFile writes a single MP4 file to OutputDir (default, no server needed).
	StreamFile StreamMode = "file"
	// StreamHLS writes a live HLS playlist + segments into OutputDir/hls.
	StreamHLS StreamMode = "hls"
	// StreamRTMP pushes an FLV stream to RTMPURL (YouTube/Twitch/custom).
	StreamRTMP StreamMode = "rtmp"
)

// AudioMode selects the audio backend.
type AudioMode string

const (
	// AudioAuto uses the wrapper for full tracks when reachable+ready, else preview.
	AudioAuto AudioMode = "auto"
	// AudioPreview always uses the 30s iTunes preview (no credentials required).
	AudioPreview AudioMode = "preview"
	// AudioFull always uses the wrapper full-track pipeline (fails if unavailable).
	AudioFull AudioMode = "full"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	// Telegram
	TelegramToken string

	// wrapper-manager gRPC endpoint. Empty disables the wrapper (preview only).
	WrapperAddr string
	WrapperTLS  bool

	// Apple catalog
	Storefront        string // e.g. "us"
	Language          string // e.g. "en-US"
	LyricsFallback    bool
	LyricsFallbackURL string

	// Audio
	AudioMode AudioMode

	// Video
	Width  int
	Height int
	FPS    int

	// Streaming
	StreamMode StreamMode
	RTMPURL    string
	OutputDir  string
	WorkDir    string

	// HTTP base URL advertised to users for HLS/file links (optional).
	PublicBaseURL string
	ListenAddr    string
	FFmpegBinary  string
	FFprobeBinary string

	// Behaviour
	MaxResults int // search results per page
}

// Default returns a config populated with defaults before env overrides.
func Default() Config {
	return Config{
		Storefront:        "us",
		Language:          "en-US",
		LyricsFallback:    true,
		LyricsFallbackURL: "https://lrclib.net",
		AudioMode:         AudioAuto,
		Width:             1920,
		Height:            1080,
		FPS:               30,
		StreamMode:        StreamFile,
		OutputDir:         "output",
		WorkDir:           "work",
		ListenAddr:        ":8088",
		FFmpegBinary:      "ffmpeg",
		FFprobeBinary:     "ffprobe",
		MaxResults:        5,
	}
}

// Load reads configuration from the process environment, applying an optional
// .env file first (real environment variables win over .env entries).
func Load() (Config, error) {
	loadDotEnv(".env")
	c := Default()

	c.TelegramToken = env("TELEGRAM_BOT_TOKEN", c.TelegramToken)
	c.WrapperAddr = env("WRAPPER_ADDR", c.WrapperAddr)
	c.WrapperTLS = envBool("WRAPPER_TLS", c.WrapperTLS)
	c.Storefront = strings.ToLower(env("STOREFRONT", c.Storefront))
	c.Language = env("LANGUAGE", c.Language)
	c.LyricsFallback = envBool("LYRICS_FALLBACK", c.LyricsFallback)
	c.LyricsFallbackURL = strings.TrimRight(env("LYRICS_FALLBACK_URL", c.LyricsFallbackURL), "/")
	c.AudioMode = AudioMode(env("AUDIO_MODE", string(c.AudioMode)))
	c.StreamMode = StreamMode(env("STREAM_MODE", string(c.StreamMode)))
	c.RTMPURL = env("RTMP_URL", c.RTMPURL)
	c.OutputDir = env("OUTPUT_DIR", c.OutputDir)
	c.WorkDir = env("WORK_DIR", c.WorkDir)
	c.PublicBaseURL = strings.TrimRight(env("PUBLIC_BASE_URL", c.PublicBaseURL), "/")
	c.ListenAddr = env("LISTEN_ADDR", c.ListenAddr)
	c.FFmpegBinary = env("FFMPEG_BINARY", c.FFmpegBinary)
	c.FFprobeBinary = env("FFPROBE_BINARY", c.FFprobeBinary)

	c.Width = envInt("VIDEO_WIDTH", c.Width)
	c.Height = envInt("VIDEO_HEIGHT", c.Height)
	c.FPS = envInt("VIDEO_FPS", c.FPS)
	c.MaxResults = envInt("MAX_RESULTS", c.MaxResults)

	return c, c.Validate()
}

// Validate checks invariants and returns a descriptive error on bad config.
func (c Config) Validate() error {
	switch c.AudioMode {
	case AudioAuto, AudioPreview, AudioFull:
	default:
		return fmt.Errorf("invalid AUDIO_MODE %q (want auto|preview|full)", c.AudioMode)
	}
	switch c.StreamMode {
	case StreamFile, StreamHLS, StreamRTMP:
	default:
		return fmt.Errorf("invalid STREAM_MODE %q (want file|hls|rtmp)", c.StreamMode)
	}
	if c.StreamMode == StreamRTMP && c.RTMPURL == "" {
		return errors.New("STREAM_MODE=rtmp requires RTMP_URL")
	}
	if c.AudioMode == AudioFull && c.WrapperAddr == "" {
		return errors.New("AUDIO_MODE=full requires WRAPPER_ADDR")
	}
	if c.Width < 320 || c.Height < 240 {
		return fmt.Errorf("video dimensions too small: %dx%d", c.Width, c.Height)
	}
	if c.FPS < 1 || c.FPS > 60 {
		return fmt.Errorf("VIDEO_FPS out of range: %d", c.FPS)
	}
	if c.MaxResults < 1 || c.MaxResults > 10 {
		return fmt.Errorf("MAX_RESULTS out of range (1-10): %d", c.MaxResults)
	}
	return nil
}

// WrapperEnabled reports whether a wrapper endpoint is configured.
func (c Config) WrapperEnabled() bool { return strings.TrimSpace(c.WrapperAddr) != "" }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
}

// loadDotEnv loads KEY=VALUE lines from path into the environment if not already
// set. Missing file is not an error.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
