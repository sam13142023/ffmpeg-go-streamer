// Package render draws the "now playing" video frame — album art, title,
// progress bar and time on the left, synced/karaoke lyrics on the right —
// matching the reference now-playing screen. It caches the static per-track
// layer so only the progress bar, times and active lyric are redrawn per frame.
package render

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/fogleman/gg"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

// lyricTransitionMS is how long the lyric column animates when a new line
// becomes active: the column scrolls one step while the new line grows and
// brightens and the old one shrinks and dims.
const lyricTransitionMS = 350

// Frame is everything needed to render one video frame.
type Frame struct {
	Track      model.Track
	Artwork    image.Image // decoded artwork, may be nil
	Lyrics     model.Lyrics
	PositionMS int
	DurationMS int
	Playing    bool
}

type layout struct {
	margin                     float64
	artX, artY, artSize        float64
	titleY, artistY            float64
	barX, barY, barW, barH     float64
	timeY                      float64
	lyrX0, lyrX1, lyrCenterY   float64
	titlePx, artistPx, timePx  float64
	lyrActivePx, lyrInactivePx float64
	emptyPx                    float64
}

// Renderer renders frames at a fixed resolution. It is safe for a single
// rendering goroutine (the player renders serially).
type Renderer struct {
	w, h  int
	fonts *fontCache
	l     layout

	mu       sync.Mutex
	curTrack string
	haveArt  bool
	base     *image.RGBA
	bgTop    color.RGBA
	bgBot    color.RGBA
}

var (
	white  = color.RGBA{0xff, 0xff, 0xff, 0xff}
	idleBG = color.RGBA{0x6d, 0x6d, 0x72, 0xff}
)

// New builds a Renderer for width x height.
func New(width, height int) (*Renderer, error) {
	fc, err := newFontCache()
	if err != nil {
		return nil, err
	}
	fh := float64(height)
	fw := float64(width)
	margin := round(fh * 0.055)

	leftX0 := margin
	leftX1 := round(fw * 0.44)
	leftW := leftX1 - leftX0
	artSize := leftW
	if s := fh * 0.52; s < artSize {
		artSize = s
	}

	timePx := fh * 0.020
	barY := (fh - margin) - (timePx + fh*0.02) // bar top
	barH := fh * 0.007
	if barH < 5 {
		barH = 5
	}

	l := layout{
		margin:  margin,
		artX:    leftX0,
		artY:    margin,
		artSize: artSize,

		barX: leftX0,
		barY: barY,
		barW: leftW,
		barH: barH,

		titleY:  barY - fh*0.075,
		artistY: barY - fh*0.030,
		timeY:   fh - margin,

		lyrX0:      round(fw * 0.50),
		lyrX1:      fw - margin,
		lyrCenterY: fh * 0.50,

		titlePx:       fh * 0.042,
		artistPx:      fh * 0.028,
		timePx:        timePx,
		lyrActivePx:   fh * 0.040,
		lyrInactivePx: fh * 0.034,
		emptyPx:       fh * 0.030,
	}

	return &Renderer{w: width, h: height, fonts: fc, l: l}, nil
}

// Size returns the frame dimensions.
func (r *Renderer) Size() (int, int) { return r.w, r.h }

// Render produces one RGBA frame. The returned image is owned by the caller.
func (r *Renderer) Render(f Frame) *image.RGBA {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.base == nil || f.Track.AdamID != r.curTrack || (f.Artwork != nil) != r.haveArt {
		r.buildBase(f)
	}

	// Clone the static base.
	dst := image.NewRGBA(r.base.Bounds())
	copy(dst.Pix, r.base.Pix)
	gc := gg.NewContextForRGBA(dst)

	r.drawProgress(gc, f)
	r.drawTimes(gc, f)
	r.drawLyrics(gc, f)
	return dst
}

// buildBase renders the static per-track layer (background, artwork, title,
// artist, empty progress track).
func (r *Renderer) buildBase(f Frame) {
	l := r.l
	base := image.NewRGBA(image.Rect(0, 0, r.w, r.h))
	gc := gg.NewContextForRGBA(base)

	// Background derived from artwork, or the idle grey.
	if f.Artwork != nil {
		avg := averageColor(f.Artwork)
		r.bgTop = scaleColor(avg, 0.42)
		r.bgBot = scaleColor(avg, 0.22)
	} else {
		r.bgTop = idleBG
		r.bgBot = scaleColor(idleBG, 0.9)
	}
	drawVerticalGradient(gc, r.w, r.h, r.bgTop, r.bgBot)

	// Artwork (rounded) or placeholder.
	radius := l.artSize * 0.045
	if f.Artwork != nil {
		art := scaleSquare(f.Artwork, int(l.artSize))
		drawShadow(gc, l.artX, l.artY, l.artSize, l.artSize, radius)
		gc.NewSubPath()
		gc.DrawRoundedRectangle(l.artX, l.artY, l.artSize, l.artSize, radius)
		gc.Clip()
		gc.DrawImage(art, int(l.artX), int(l.artY))
		gc.ResetClip()
	} else {
		drawShadow(gc, l.artX, l.artY, l.artSize, l.artSize, radius)
		gc.SetColor(color.RGBA{0xe6, 0xe6, 0xe8, 0xff})
		gc.DrawRoundedRectangle(l.artX, l.artY, l.artSize, l.artSize, radius)
		gc.Fill()
		drawMusicNote(gc, l.artX, l.artY, l.artSize, color.RGBA{0xb2, 0xb2, 0xb6, 0xff})
	}

	// Title + artist.
	title := f.Track.Title
	if title == "" {
		title = "Not Playing"
	}
	gc.SetFontFace(r.fonts.face(l.titlePx, true))
	gc.SetColor(white)
	gc.DrawStringAnchored(truncate(gc, title, l.barW), l.barX, l.titleY, 0, 0.5)

	if f.Track.Artist != "" {
		gc.SetFontFace(r.fonts.face(l.artistPx, false))
		gc.SetRGBA(1, 1, 1, 0.7)
		gc.DrawStringAnchored(truncate(gc, f.Track.Artist, l.barW), l.barX, l.artistY, 0, 0.5)
	}

	// Empty progress track.
	gc.SetRGBA(1, 1, 1, 0.22)
	gc.DrawRoundedRectangle(l.barX, l.barY, l.barW, l.barH, l.barH/2)
	gc.Fill()

	r.base = base
	r.curTrack = f.Track.AdamID
	r.haveArt = f.Artwork != nil
}

func (r *Renderer) drawProgress(gc *gg.Context, f Frame) {
	l := r.l
	if f.DurationMS <= 0 || !f.Playing {
		return
	}
	frac := float64(f.PositionMS) / float64(f.DurationMS)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	fillW := l.barW * frac
	if fillW > 0 {
		gc.SetRGBA(1, 1, 1, 0.95)
		gc.DrawRoundedRectangle(l.barX, l.barY, fillW, l.barH, l.barH/2)
		gc.Fill()
		// Playhead knob.
		knobR := l.barH * 1.4
		gc.DrawCircle(l.barX+fillW, l.barY+l.barH/2, knobR)
		gc.Fill()
	}
}

func (r *Renderer) drawTimes(gc *gg.Context, f Frame) {
	l := r.l
	gc.SetFontFace(r.fonts.face(l.timePx, false))
	gc.SetRGBA(1, 1, 1, 0.55)

	elapsed, remaining := "--:--", "--:--"
	if f.DurationMS > 0 && f.Playing {
		pos := f.PositionMS
		if pos > f.DurationMS {
			pos = f.DurationMS
		}
		elapsed = formatMS(pos)
		remaining = "-" + formatMS(f.DurationMS-pos)
	}
	gc.DrawStringAnchored(elapsed, l.barX, l.timeY, 0, 0.5)
	gc.DrawStringAnchored(remaining, l.barX+l.barW, l.timeY, 1, 0.5)
}

func (r *Renderer) drawLyrics(gc *gg.Context, f Frame) {
	l := r.l
	maxW := l.lyrX1 - l.lyrX0
	centerX := (l.lyrX0 + l.lyrX1) / 2

	if f.Lyrics.Empty() {
		msg := "Play a song to see lyrics here."
		if f.Playing && f.Track.Title != "" {
			msg = "Lyrics not available for this track."
		}
		gc.SetFontFace(r.fonts.face(l.emptyPx, false))
		gc.SetRGBA(1, 1, 1, 0.85)
		gc.DrawStringAnchored(msg, centerX, l.lyrCenterY, 0.5, 0.5)
		return
	}

	active := f.Lyrics.ActiveLineAt(f.PositionMS)
	prog := 1.0
	if active >= 0 {
		prog = transitionProgress(f.Lyrics.Lines[active].StartMS, f.PositionMS)
	}
	anchor := scrollAnchor(active, prog)
	step := l.lyrInactivePx * 1.62
	top := l.margin
	bottom := float64(r.h) - l.margin

	for i := range f.Lyrics.Lines {
		y := l.lyrCenterY + (float64(i)-anchor)*step
		if y < top-step || y > bottom+step {
			continue
		}
		fade := edgeFade(y, top, bottom, step)
		if fade <= 0 {
			continue
		}
		line := f.Lyrics.Lines[i]
		isActive := i == active
		px := l.lyrInactivePx
		var alpha float64
		switch {
		case isActive:
			px = lerp(l.lyrInactivePx, l.lyrActivePx, prog)
			alpha = lerp(0.4, 1, prog)
		case i == active-1:
			// The line that just finished eases back down to resting style.
			px = lerp(l.lyrActivePx, l.lyrInactivePx, prog)
			alpha = lerp(1, 0.4, prog)
		case i < active:
			alpha = 0.4
		default:
			alpha = 0.3
		}
		gc.SetFontFace(r.fonts.face(px, isActive))

		switch {
		case isActive && len(line.Words) > 0:
			full := truncate(gc, joinWords(line.Words, len(line.Words)), maxW)
			gc.SetRGBA(1, 1, 1, 0.5*fade)
			gc.DrawStringAnchored(full, l.lyrX0, y, 0, 0.5)
			sw := sungWidth(gc, line.Words, f.PositionMS)
			if w, _ := gc.MeasureString(full); sw > w {
				sw = w
			}
			if sw > 0 {
				// Karaoke sweep: reveal the sung part of the line through a
				// clip window that slides across the text within each word.
				gc.DrawRectangle(l.lyrX0-2, y-px, sw+2, px*2)
				gc.Clip()
				gc.SetRGBA(1, 1, 1, fade)
				gc.DrawStringAnchored(full, l.lyrX0, y, 0, 0.5)
				gc.ResetClip()
			}
		default:
			gc.SetRGBA(1, 1, 1, alpha*fade)
			gc.DrawStringAnchored(truncate(gc, line.Text, maxW), l.lyrX0, y, 0, 0.5)
		}
	}
}

// transitionProgress maps time since a line activated onto an eased 0..1.
func transitionProgress(startMS, posMS int) float64 {
	dt := posMS - startMS
	if dt <= 0 {
		return 0
	}
	if dt >= lyricTransitionMS {
		return 1
	}
	return easeOutCubic(float64(dt) / lyricTransitionMS)
}

// scrollAnchor is the fractional line index the lyric column centers on. While
// a new line is activating it eases from the previous line to the new one.
func scrollAnchor(active int, prog float64) float64 {
	if active <= 0 {
		return 0
	}
	return float64(active-1) + prog
}

func easeOutCubic(t float64) float64 {
	u := 1 - t
	return 1 - u*u*u
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// edgeFade dims lines as they approach the top/bottom of the lyric column so
// scrolled lines slide out gradually instead of clipping hard.
func edgeFade(y, top, bottom, step float64) float64 {
	d := math.Min(y-top, bottom-y)
	if d >= step {
		return 1
	}
	if d <= 0 {
		return 0
	}
	return d / step
}

// --- drawing helpers ---

func drawVerticalGradient(gc *gg.Context, w, h int, top, bot color.RGBA) {
	fh := float64(h)
	for y := 0; y < h; y++ {
		t := float64(y) / fh
		gc.SetColor(mixColor(top, bot, t))
		gc.DrawRectangle(0, float64(y), float64(w), 1)
		gc.Fill()
	}
}

func drawShadow(gc *gg.Context, x, y, w, h, r float64) {
	// A soft drop shadow approximated by a few offset translucent rounded rects.
	for i := 6; i >= 1; i-- {
		off := float64(i) * 2
		gc.SetRGBA(0, 0, 0, 0.03)
		gc.DrawRoundedRectangle(x-off*0.3, y+off, w+off*0.6, h, r)
		gc.Fill()
	}
}

// drawMusicNote draws a simple stylized eighth-note pair inside the box.
func drawMusicNote(gc *gg.Context, x, y, size float64, c color.RGBA) {
	cx := x + size*0.5
	cy := y + size*0.5
	s := size * 0.32
	gc.SetColor(c)
	// Note heads.
	headR := s * 0.22
	gc.DrawEllipse(cx-s*0.42, cy+s*0.55, headR*1.25, headR)
	gc.Fill()
	gc.DrawEllipse(cx+s*0.42, cy+s*0.30, headR*1.25, headR)
	gc.Fill()
	// Stems.
	stemW := s * 0.09
	gc.DrawRectangle(cx-s*0.42+headR*1.05, cy-s*0.9, stemW, s*1.5)
	gc.Fill()
	gc.DrawRectangle(cx+s*0.42+headR*1.05, cy-s*1.15, stemW, s*1.5)
	gc.Fill()
	// Beam.
	gc.SetLineWidth(stemW * 1.6)
	gc.DrawLine(cx-s*0.42+headR*1.05+stemW/2, cy-s*0.9, cx+s*0.42+headR*1.05+stemW/2, cy-s*1.15)
	gc.Stroke()
}

// truncate shortens s with an ellipsis so it fits maxW under the current face.
func truncate(gc *gg.Context, s string, maxW float64) string {
	if w, _ := gc.MeasureString(s); w <= maxW {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		cand := string(runes) + "…"
		if w, _ := gc.MeasureString(cand); w <= maxW {
			return cand
		}
	}
	return "…"
}

func joinWords(words []model.LyricWord, n int) string {
	if n > len(words) {
		n = len(words)
	}
	out := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			out += " "
		}
		out += words[i].Text
	}
	return out
}

// sungWidth returns how many pixels of the joined line are sung at posMS,
// interpolating inside the word currently being sung so the karaoke highlight
// sweeps smoothly instead of jumping word by word.
func sungWidth(gc *gg.Context, words []model.LyricWord, posMS int) float64 {
	n := 0
	for n < len(words) && effectiveWordEnd(words, n) <= posMS {
		n++
	}
	done, _ := gc.MeasureString(joinWords(words, n))
	if n >= len(words) || words[n].StartMS > posMS {
		return done
	}
	w := words[n]
	end := effectiveWordEnd(words, n)
	t := 1.0
	if end > w.StartMS {
		t = float64(posMS-w.StartMS) / float64(end-w.StartMS)
	}
	next, _ := gc.MeasureString(joinWords(words, n+1))
	return done + (next-done)*math.Min(t, 1)
}

// effectiveWordEnd returns when word i stops being "in progress". Words with
// no usable end timing borrow the next word's start, or a short default.
func effectiveWordEnd(words []model.LyricWord, i int) int {
	w := words[i]
	if w.EndMS > w.StartMS {
		return w.EndMS
	}
	if i+1 < len(words) && words[i+1].StartMS > w.StartMS {
		return words[i+1].StartMS
	}
	return w.StartMS + 400
}

func formatMS(ms int) string {
	if ms < 0 {
		ms = 0
	}
	sec := ms / 1000
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

func round(v float64) float64 {
	return float64(int(v + 0.5))
}
