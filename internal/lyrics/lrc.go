package lyrics

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

// ParseLRC converts standard [mm:ss.xx] synchronized lyrics into the common
// lyric model used by the video renderer. Multiple timestamps on one line are
// supported. Metadata tags such as [ar:] and [ti:] are ignored.
func ParseLRC(raw string, durationMS int) (model.Lyrics, error) {
	var lines []model.LyricLine
	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimSpace(rawLine)
		var starts []int
		for strings.HasPrefix(line, "[") {
			end := strings.IndexByte(line, ']')
			if end < 0 {
				break
			}
			tag := line[1:end]
			if ms, ok := parseLRCTime(tag); ok {
				starts = append(starts, ms)
			}
			line = strings.TrimSpace(line[end+1:])
		}
		if line == "" || len(starts) == 0 {
			continue
		}
		for _, start := range starts {
			lines = append(lines, model.LyricLine{StartMS: start, Text: line})
		}
	}
	if len(lines) == 0 {
		return model.Lyrics{}, fmt.Errorf("%w: invalid or empty synchronized LRC", ErrNotFound)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].StartMS < lines[j].StartMS })
	for i := range lines {
		if i+1 < len(lines) {
			lines[i].EndMS = lines[i+1].StartMS
		} else if durationMS > lines[i].StartMS {
			lines[i].EndMS = durationMS
		} else {
			lines[i].EndMS = lines[i].StartMS + 5000
		}
	}
	return model.Lyrics{Lines: lines, Synced: true}, nil
}

func parseLRCTime(tag string) (int, bool) {
	parts := strings.Split(tag, ":")
	if len(parts) != 2 {
		return 0, false
	}
	minutes, err := strconv.Atoi(parts[0])
	if err != nil || minutes < 0 {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || seconds < 0 || seconds >= 60 {
		return 0, false
	}
	return minutes*60*1000 + int(seconds*1000+0.5), true
}
