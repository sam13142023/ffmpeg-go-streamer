package stream

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sam1314/ffmpeg-go-streamer/internal/config"
	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
	"github.com/sam1314/ffmpeg-go-streamer/internal/render"
)

func TestFFmpegCreatesPlayableMP4(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	wav := filepath.Join(dir, "tone.wav")
	writeToneWAV(t, wav, 500)
	r, err := render.New(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	s := NewFFmpeg(ffmpeg, config.StreamFile, "", dir, 4)
	result, err := s.Play(context.Background(), r, render.Frame{
		Track: model.Track{AdamID: "test", Title: "Test Tone", Artist: "Codex"},
	}, model.NewAudio(wav, 500, "test", nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(result.Path)
	if err != nil || info.Size() < 1000 {
		t.Fatalf("bad output: path=%q info=%v err=%v", result.Path, info, err)
	}
}

func writeToneWAV(t *testing.T, path string, durationMS int) {
	t.Helper()
	const sampleRate = 48000
	samples := sampleRate * durationMS / 1000
	dataSize := samples * 2
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	write := func(v any) {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = f.WriteString("RIFF")
	write(uint32(36 + dataSize))
	_, _ = f.WriteString("WAVEfmt ")
	write(uint32(16))
	write(uint16(1))
	write(uint16(1))
	write(uint32(sampleRate))
	write(uint32(sampleRate * 2))
	write(uint16(2))
	write(uint16(16))
	_, _ = f.WriteString("data")
	write(uint32(dataSize))
	for i := 0; i < samples; i++ {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/sampleRate) * 5000)
		write(v)
	}
}
