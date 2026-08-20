package render

import (
	"image/color"
	"testing"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

func TestRenderNowPlayingFrame(t *testing.T) {
	r, err := New(640, 360)
	if err != nil {
		t.Fatal(err)
	}
	img := r.Render(Frame{
		Track: model.Track{AdamID: "1", Title: "Yellow", Artist: "Coldplay"},
		Lyrics: model.Lyrics{Synced: true, Lines: []model.LyricLine{
			{StartMS: 0, EndMS: 2000, Text: "Look at the stars"},
			{StartMS: 2000, EndMS: 4000, Text: "Look how they shine for you"},
		}},
		PositionMS: 2500, DurationMS: 4000, Playing: true,
	})
	if img.Bounds().Dx() != 640 || img.Bounds().Dy() != 360 {
		t.Fatalf("unexpected bounds %v", img.Bounds())
	}
	first := color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA)
	different := false
	for y := 0; y < 360 && !different; y += 20 {
		for x := 0; x < 640; x += 20 {
			if color.RGBAModel.Convert(img.At(x, y)).(color.RGBA) != first {
				different = true
				break
			}
		}
	}
	if !different {
		t.Fatal("rendered frame is a single flat color")
	}
}

func TestLyricTransitionMath(t *testing.T) {
	if got := transitionProgress(2000, 1500); got != 0 {
		t.Fatalf("before activation: progress = %v", got)
	}
	mid := transitionProgress(2000, 2000+lyricTransitionMS/2)
	if mid <= 0 || mid >= 1 {
		t.Fatalf("mid transition: progress = %v", mid)
	}
	if got := transitionProgress(2000, 2000+lyricTransitionMS); got != 1 {
		t.Fatalf("after transition: progress = %v", got)
	}

	if got := scrollAnchor(-1, 1); got != 0 {
		t.Fatalf("no active line: anchor = %v", got)
	}
	if got := scrollAnchor(3, 0); got != 2 {
		t.Fatalf("transition start: anchor = %v", got)
	}
	if got := scrollAnchor(3, 1); got != 3 {
		t.Fatalf("transition end: anchor = %v", got)
	}

	if got := edgeFade(100, 0, 200, 50); got != 1 {
		t.Fatalf("center fade = %v", got)
	}
	if got := edgeFade(25, 0, 200, 50); got != 0.5 {
		t.Fatalf("near-edge fade = %v", got)
	}
	if got := edgeFade(-10, 0, 200, 50); got != 0 {
		t.Fatalf("off-screen fade = %v", got)
	}
}

func TestEffectiveWordEnd(t *testing.T) {
	words := []model.LyricWord{
		{Text: "Look", StartMS: 0, EndMS: 300},
		{Text: "at", StartMS: 300},  // no end: borrows next start
		{Text: "the", StartMS: 500}, // no end, last-ish: default span
	}
	if got := effectiveWordEnd(words, 0); got != 300 {
		t.Fatalf("timed word end = %d", got)
	}
	if got := effectiveWordEnd(words, 1); got != 500 {
		t.Fatalf("untimed word should borrow next start, got %d", got)
	}
	if got := effectiveWordEnd(words, 2); got != 900 {
		t.Fatalf("last untimed word should use default span, got %d", got)
	}
}
