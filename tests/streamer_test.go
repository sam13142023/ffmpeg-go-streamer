package ffmpeg_go_streamer_test

import (
	"os"
	"testing"
	"time"
)

func TestNewStreamer(t *testing.T) {
	streamer := NewStreamer()
	if streamer == nil {
		t.Fatal("NewStreamer() returned nil")
	}
	if streamer.ffmpegPath != "ffmpeg" {
		t.Errorf("Expected default ffmpegPath to be 'ffmpeg', got %s", streamer.ffmpegPath)
	}
}

func TestSetFFmpegPath(t *testing.T) {
	streamer := NewStreamer()
	customPath := "/usr/local/bin/ffmpeg"
	streamer.SetFFmpegPath(customPath)
	if streamer.ffmpegPath != customPath {
		t.Errorf("Expected ffmpegPath to be %s, got %s", customPath, streamer.ffmpegPath)
	}
}

func TestSetTimeout(t *testing.T) {
	streamer := NewStreamer()
	customTimeout := 60 * time.Second
	streamer.SetTimeout(customTimeout)
	if streamer.timeout != customTimeout {
		t.Errorf("Expected timeout to be %v, got %v", customTimeout, streamer.timeout)
	}
}

func TestGetDefaultOptions(t *testing.T) {
	options := getDefaultOptions()
	if options.VideoCodec != "libx264" {
		t.Errorf("Expected default VideoCodec to be 'libx264', got %s", options.VideoCodec)
	}
	if options.AudioCodec != "aac" {
		t.Errorf("Expected default AudioCodec to be 'aac', got %s", options.AudioCodec)
	}
	if options.RetryCount != 3 {
		t.Errorf("Expected default RetryCount to be 3, got %d", options.RetryCount)
	}
}

func TestNewRTMPSStreamer(t *testing.T) {
	config := &RTMPSConfig{
		Server:    "rtmps://test.example.com/live",
		StreamKey: "test-key",
		TLSVerify: true,
	}

	streamer := NewRTMPSStreamer(config)
	if streamer == nil {
		t.Fatal("NewRTMPSStreamer() returned nil")
	}
	if streamer.config != config {
		t.Error("RTMPS config not set correctly")
	}
}

func TestBuildRTMPSURL(t *testing.T) {
	tests := []struct {
		name     string
		config   *RTMPSConfig
		expected string
		hasError bool
	}{
		{
			name: "Basic RTMPS URL",
			config: &RTMPSConfig{
				Server:    "rtmps://live.example.com/app",
				StreamKey: "stream123",
			},
			expected: "rtmps://live.example.com/app/stream123",
			hasError: false,
		},
		{
			name: "RTMPS URL with auth",
			config: &RTMPSConfig{
				Server:    "rtmps://live.example.com/app",
				StreamKey: "stream123",
				Username:  "user",
				Password:  "pass",
			},
			expected: "rtmps://user:pass@live.example.com/app/stream123",
			hasError: false,
		},
		{
			name: "Convert RTMP to RTMPS",
			config: &RTMPSConfig{
				Server:    "rtmp://live.example.com/app",
				StreamKey: "stream123",
			},
			expected: "rtmps://live.example.com/app/stream123",
			hasError: false,
		},
		{
			name: "No server",
			config: &RTMPSConfig{
				StreamKey: "stream123",
			},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			streamer := NewRTMPSStreamer(tt.config)
			url, err := streamer.buildRTMPSURL()

			if tt.hasError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if url != tt.expected {
				t.Errorf("Expected URL %s, got %s", tt.expected, url)
			}
		})
	}
}

func TestNewMerger(t *testing.T) {
	merger := NewMerger()
	if merger == nil {
		t.Fatal("NewMerger() returned nil")
	}
	if merger.Streamer == nil {
		t.Error("Merger.Streamer is nil")
	}
}

func TestGetDefaultMergeConfig(t *testing.T) {
	config := getDefaultMergeConfig()
	if config.ImageDuration != 10.0 {
		t.Errorf("Expected default ImageDuration to be 10.0, got %f", config.ImageDuration)
	}
	if config.ImageScale != "1920:1080" {
		t.Errorf("Expected default ImageScale to be '1920:1080', got %s", config.ImageScale)
	}
	if !config.AudioLoop {
		t.Error("Expected default AudioLoop to be true")
	}
}

// 集成测试（需要实际的 FFmpeg 安装）
func TestFFmpegAvailability(t *testing.T) {
	if os.Getenv("SKIP_INTEGRATION_TESTS") != "" {
		t.Skip("Skipping integration test")
	}

	streamer := NewStreamer()
	err := streamer.checkFFmpeg()
	if err != nil {
		t.Logf("FFmpeg not available: %v", err)
		t.Skip("FFmpeg not installed, skipping test")
	}
}
