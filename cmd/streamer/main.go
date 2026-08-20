package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sam1314/ffmpeg-go-streamer/internal/applemusic"
	"github.com/sam1314/ffmpeg-go-streamer/internal/audio"
	"github.com/sam1314/ffmpeg-go-streamer/internal/config"
	"github.com/sam1314/ffmpeg-go-streamer/internal/lyrics"
	"github.com/sam1314/ffmpeg-go-streamer/internal/media"
	"github.com/sam1314/ffmpeg-go-streamer/internal/player"
	"github.com/sam1314/ffmpeg-go-streamer/internal/render"
	"github.com/sam1314/ffmpeg-go-streamer/internal/server"
	"github.com/sam1314/ffmpeg-go-streamer/internal/stream"
	"github.com/sam1314/ffmpeg-go-streamer/internal/telegram"
	"github.com/sam1314/ffmpeg-go-streamer/internal/wrapper"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath(cfg.FFmpegBinary); err != nil {
		return fmt.Errorf("FFmpeg not found (%s): %w", cfg.FFmpegBinary, err)
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		return err
	}

	renderer, err := render.New(cfg.Width, cfg.Height)
	if err != nil {
		return err
	}
	idle := renderer.Render(render.Frame{})
	if err := stream.WritePNG(filepath.Join(cfg.OutputDir, "idle.png"), idle); err != nil {
		return fmt.Errorf("write idle frame: %w", err)
	}

	catalog := applemusic.New(cfg.Storefront)
	preview := audio.NewPreviewResolver(cfg.WorkDir, cfg.FFprobeBinary)
	var resolver audio.Resolver = preview
	lyricProviders := make([]lyrics.Provider, 0, 2)
	if cfg.LyricsFallback {
		lyricProviders = append(lyricProviders, lyrics.NewLRCLibProvider(cfg.LyricsFallbackURL))
	}
	var wrapperClient *wrapper.Client
	if cfg.WrapperEnabled() {
		wrapperClient, err = wrapper.DialTLS(cfg.WrapperAddr, cfg.WrapperTLS)
		if err != nil {
			return err
		}
		defer wrapperClient.Close()
		full := audio.NewWrapperResolver(wrapperClient, cfg.WorkDir, cfg.FFmpegBinary, cfg.FFprobeBinary)
		switch cfg.AudioMode {
		case config.AudioPreview:
			resolver = preview
		case config.AudioFull:
			resolver = full
		default:
			resolver = audio.FallbackResolver{Primary: full, Secondary: preview}
		}
		// Apple/wrapper lyrics are preferred because they may include word-level
		// timing. LRCLIB remains behind it as the line-synced fallback.
		lyricProviders = append([]lyrics.Provider{lyrics.NewWrapperProvider(wrapperClient, cfg.Language)}, lyricProviders...)
	}
	var lyricProvider lyrics.Provider = lyrics.EmptyProvider{}
	if len(lyricProviders) > 0 {
		lyricProvider = lyrics.FallbackProvider{Providers: lyricProviders}
	}

	artwork := media.NewArtworkLoader(min(cfg.Width, cfg.Height))
	ffmpeg := stream.NewFFmpeg(cfg.FFmpegBinary, cfg.StreamMode, cfg.RTMPURL, cfg.OutputDir, cfg.FPS)
	p := player.New(resolver, lyricProvider, artwork, renderer, ffmpeg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go p.Run(ctx)

	httpServer := server.New(cfg.ListenAddr, cfg.OutputDir, cfg.StreamMode, p)
	errCh := make(chan error, 2)
	go func() {
		slog.Info("web server listening", "address", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	if cfg.TelegramToken != "" {
		bot, err := telegram.New(cfg.TelegramToken, catalog, p, cfg.MaxResults)
		if err != nil {
			return err
		}
		go func() { errCh <- bot.Run(ctx) }()
	} else {
		slog.Warn("TELEGRAM_BOT_TOKEN is empty; Telegram commands are disabled")
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			stop()
			_ = httpServer.Shutdown()
			return err
		}
	}
	return httpServer.Shutdown()
}
