package render

import (
	"sync"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// fontCache lazily builds and caches truetype faces at specific pixel sizes for
// the two weights we use. Faces are cached because constructing them per frame
// is wasteful and truetype.Face is safe to reuse for drawing on a single
// goroutine (rendering is serialized per Renderer).
type fontCache struct {
	mu      sync.Mutex
	regular *truetype.Font
	bold    *truetype.Font
	faces   map[faceKey]font.Face
}

type faceKey struct {
	bold bool
	size int // rounded px
}

func newFontCache() (*fontCache, error) {
	reg, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	bd, err := truetype.Parse(gobold.TTF)
	if err != nil {
		return nil, err
	}
	return &fontCache{regular: reg, bold: bd, faces: map[faceKey]font.Face{}}, nil
}

// face returns a cached face for the requested weight and pixel size.
func (fc *fontCache) face(size float64, bold bool) font.Face {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	key := faceKey{bold: bold, size: int(size + 0.5)}
	if f, ok := fc.faces[key]; ok {
		return f
	}
	src := fc.regular
	if bold {
		src = fc.bold
	}
	f := truetype.NewFace(src, &truetype.Options{Size: size, DPI: 72, Hinting: font.HintingFull})
	fc.faces[key] = f
	return f
}
