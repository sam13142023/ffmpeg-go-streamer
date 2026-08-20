# Live Music Video Streamer

A Go application that turns Telegram song requests into a shared live music-video stream. Users search with `/search`, choose a numbered inline button, and the app renders album art, track details, progress, and synchronized lyrics into MP4, HLS, or RTMP using FFmpeg.

The visual layout follows the supplied reference: artwork and playback status on the left, large scrolling lyrics on the right, a color palette derived from the cover, and karaoke-style word highlighting when wrapper-manager returns syllable timing.

## What is included

- Telegram commands: `/search`, `/status`, `/nowplaying`, `/queue`, `/pause`, `/resume`, `/next` (`/skip`), `/replay`, and `/help`, plus inline pause/next/replay buttons on `/status`
- Numbered inline results with previous/next pagination
- Apple catalog search, artwork, metadata, and public preview fallback
- Native gRPC integration with [WorldObservationLog/wrapper-manager](https://github.com/WorldObservationLog/wrapper-manager) for readiness, enhanced-HLS assets, and synchronized TTML lyrics
- Automatic lyrics fallback to LRCLIB when Apple Music/wrapper lyrics return 404 or time out
- A single concurrency-safe radio queue shared by all Telegram users
- 1920×1080 Go frame renderer with artwork-derived gradients and karaoke highlighting
- FFmpeg outputs: per-track MP4, live HLS, or RTMP for YouTube/Twitch/custom servers
- Built-in web page, `/healthz`, `/api/status`, and `/media/` output hosting
- Docker Compose deployment with wrapper-manager as a separate service

## Quick start with Docker

Requirements: Docker Compose and a Telegram bot token from `@BotFather`.

```sh
cp .env.example .env
# Set TELEGRAM_BOT_TOKEN in .env
docker compose up --build
```

Open `http://localhost:8088`, then message the bot:

```text
/search yellow
```

Choose one of the numbered buttons. With the default `STREAM_MODE=hls`, Safari can play the live HLS source directly at `http://localhost:8088/media/hls/stream.m3u8`. Other players such as VLC can open the same URL.

The local wrapper-manager starts without an Apple account. Its lyrics endpoint can work without an account; full protected audio availability depends on how that wrapper-manager deployment is configured. See the upstream login instructions when an account-backed instance is required.

Lyrics are resolved in this order: Apple Music via wrapper-manager, then LRCLIB synced LRC, then plain LRCLIB lyrics. A missing or slow lyrics response never stops audio playback or the queue; the video shows “Lyrics not available for this track” instead.

## Run locally

Install Go 1.25+ and FFmpeg/FFprobe, then:

```sh
cp .env.example .env
# For a wrapper running on your machine:
# WRAPPER_ADDR=localhost:8080
go run ./cmd/streamer
```

The app still runs if `TELEGRAM_BOT_TOKEN` is blank, but only the status/media web server is enabled. The idle reference frame is written to `output/idle.png` at startup.

## Audio behavior

`AUDIO_MODE` controls track acquisition:

- `auto` tries the wrapper-manager enhanced-HLS asset first and falls back to Apple's public 30-second catalog preview if the asset is unavailable or still DRM-protected.
- `preview` always uses the public preview. This is the easiest zero-credential development mode.
- `full` requires wrapper-manager to return an asset FFmpeg can read. It fails the selected track instead of silently using a preview.

wrapper-manager exposes sample decryption primitives, not a finished audio file. Some deployments return protected FairPlay HLS that requires a separate reconstruction client; FFmpeg alone cannot decrypt those assets. This app deliberately reports that failure and uses the preview in `auto` mode rather than pretending the full track played. Use music only where you have the necessary rights and authorization.

## Streaming modes

### HLS

```env
STREAM_MODE=hls
```

Writes `output/hls/stream.m3u8` and a rolling segment window. The built-in server exposes it under `/media/hls/stream.m3u8`.

### RTMP

```env
STREAM_MODE=rtmp
RTMP_URL=rtmp://server.example/live/your-stream-key
```

The stream key remains in `.env`, which is ignored by Git.

### MP4 files

```env
STREAM_MODE=file
```

Writes one timestamped MP4 into `output/` for every completed selection.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | empty | Token from `@BotFather`; empty disables Telegram |
| `WRAPPER_ADDR` | empty | wrapper-manager gRPC `host:port` |
| `WRAPPER_TLS` | `false` | Enable TLS for a public wrapper endpoint |
| `STOREFRONT` | `us` | Apple catalog/storefront country |
| `LANGUAGE` | `en-US` | Requested lyrics/metadata language |
| `LYRICS_FALLBACK` | `true` | Use LRCLIB after Apple/wrapper lyrics fail |
| `LYRICS_FALLBACK_URL` | `https://lrclib.net` | LRCLIB API base URL or compatible mirror |
| `AUDIO_MODE` | `auto` | `auto`, `preview`, or `full` |
| `STREAM_MODE` | `file` | `file`, `hls`, or `rtmp` |
| `RTMP_URL` | empty | RTMP destination for `rtmp` mode |
| `LISTEN_ADDR` | `:8088` | Built-in HTTP listener |
| `VIDEO_WIDTH` / `VIDEO_HEIGHT` | `1920` / `1080` | Output resolution |
| `VIDEO_FPS` | `30` | Frame rate, 1–60 |
| `MAX_RESULTS` | `5` | Telegram results per page, 1–10 |
| `OUTPUT_DIR` / `WORK_DIR` | `output` / `work` | Generated and temporary files |
| `FFMPEG_BINARY` / `FFPROBE_BINARY` | command names | Override executable paths |

## Verification

The test suite includes TTML timing, renderer layout, Telegram keyboard/session behavior, and a real short FFmpeg encode generated entirely from local test audio:

```sh
go test ./...
go vet ./...
```

## Project layout

```text
cmd/streamer       application entry point
internal/audio     wrapper/preview audio resolution
internal/telegram  commands, search sessions, inline keyboards
internal/player    shared playback queue and lifecycle
internal/render    screenshot-inspired Go video frames
internal/stream    FFmpeg MP4/HLS/RTMP encoder
internal/lyrics    wrapper TTML parser/provider
internal/wrapper   generated gRPC client for wrapper-manager
```
