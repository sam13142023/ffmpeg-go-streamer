// Package lyrics parses Apple Music syllable-TTML into timed lines and provides
// lyric lookup backed by the wrapper (with a graceful empty fallback).
package lyrics

import (
	"encoding/xml"
	"sort"
	"strings"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

// ParseTTML parses an Apple Music TTML document (as returned by the wrapper's
// Lyrics RPC / amp-api syllable-lyrics) into ordered, timed lines.
//
// It is deliberately namespace-agnostic: <p> elements become lines and nested
// <span> elements become word timings, reading begin/end attributes regardless
// of their namespace prefix. If no timed <p> elements are found it falls back to
// treating each non-empty text line as an untimed lyric line.
func ParseTTML(ttml string) (model.Lyrics, error) {
	ttml = strings.TrimSpace(ttml)
	if ttml == "" {
		return model.Lyrics{}, nil
	}

	dec := xml.NewDecoder(strings.NewReader(ttml))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var (
		lines    []model.LyricLine
		lang     string
		inP      bool
		inSpan   bool
		cur      model.LyricLine
		curWord  model.LyricWord
		lineBuf  strings.Builder
		spanBuf  strings.Builder
		anyTimed bool
	)

	for {
		tok, err := dec.Token()
		if err != nil {
			break // io.EOF or malformed tail; use whatever we parsed
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(local(t.Name.Local))
			switch name {
			case "tt":
				if v := attr(t, "lang"); v != "" {
					lang = v
				}
			case "p":
				inP = true
				cur = model.LyricLine{
					StartMS: parseTime(attr(t, "begin")),
					EndMS:   parseTime(attr(t, "end")),
				}
				lineBuf.Reset()
			case "span":
				if inP {
					inSpan = true
					curWord = model.LyricWord{
						StartMS: parseTime(attr(t, "begin")),
						EndMS:   parseTime(attr(t, "end")),
					}
					spanBuf.Reset()
				}
			case "br":
				if inP {
					lineBuf.WriteByte(' ')
				}
			}
		case xml.CharData:
			s := string(t)
			if inSpan {
				spanBuf.WriteString(s)
			}
			if inP {
				lineBuf.WriteString(s)
			}
		case xml.EndElement:
			name := strings.ToLower(local(t.Name.Local))
			switch name {
			case "span":
				if inSpan {
					curWord.Text = collapseSpaces(spanBuf.String())
					if curWord.Text != "" {
						cur.Words = append(cur.Words, curWord)
						if curWord.StartMS > 0 || curWord.EndMS > 0 {
							anyTimed = true
						}
					}
					inSpan = false
				}
			case "p":
				if inP {
					cur.Text = collapseSpaces(lineBuf.String())
					// Derive line timing from words when the <p> lacked it.
					if cur.StartMS == 0 && len(cur.Words) > 0 {
						cur.StartMS = cur.Words[0].StartMS
					}
					if cur.EndMS == 0 && len(cur.Words) > 0 {
						cur.EndMS = cur.Words[len(cur.Words)-1].EndMS
					}
					if cur.Text != "" {
						lines = append(lines, cur)
						if cur.StartMS > 0 || cur.EndMS > 0 {
							anyTimed = true
						}
					}
					inP = false
				}
			}
		}
	}

	if len(lines) == 0 {
		// Not TTML (or unparseable structure): treat as plain text.
		return plainText(ttml, lang), nil
	}

	sort.SliceStable(lines, func(i, j int) bool { return lines[i].StartMS < lines[j].StartMS })
	return model.Lyrics{Lines: lines, Synced: anyTimed, Language: lang}, nil
}

// plainText builds an untimed Lyrics value from raw newline-separated text.
func plainText(s, lang string) model.Lyrics {
	var lines []model.LyricLine
	for _, raw := range strings.Split(s, "\n") {
		txt := collapseSpaces(raw)
		if txt == "" || strings.HasPrefix(txt, "<") {
			continue
		}
		lines = append(lines, model.LyricLine{Text: txt})
	}
	return model.Lyrics{Lines: lines, Synced: false, Language: lang}
}

// local strips any namespace prefix from an XML local name.
func local(name string) string {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// attr returns the value of the attribute whose local name matches key
// (case-insensitive), ignoring namespace prefixes.
func attr(e xml.StartElement, key string) string {
	key = strings.ToLower(key)
	for _, a := range e.Attr {
		if strings.ToLower(local(a.Name.Local)) == key {
			return a.Value
		}
	}
	return ""
}

// parseTime parses a TTML clock/offset value into milliseconds. Accepts
// "hh:mm:ss.mmm", "mm:ss.mmm", "ss.mmm", and offset forms like "12.5s"/"400ms".
func parseTime(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	// Offset-time with unit suffix.
	if strings.HasSuffix(v, "ms") {
		return int(parseFloat(strings.TrimSuffix(v, "ms")))
	}
	if strings.HasSuffix(v, "s") && !strings.Contains(v, ":") {
		return int(parseFloat(strings.TrimSuffix(v, "s")) * 1000)
	}
	// Colon-separated clock time.
	parts := strings.Split(v, ":")
	var seconds float64
	for _, p := range parts {
		seconds = seconds*60 + parseFloat(p)
	}
	return int(seconds * 1000)
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	var whole, frac float64
	var fracDiv float64 = 1
	seenDot := false
	neg := false
	for i, r := range s {
		switch {
		case r == '-' && i == 0:
			neg = true
		case r == '.':
			seenDot = true
		case r >= '0' && r <= '9':
			if seenDot {
				fracDiv *= 10
				frac = frac*10 + float64(r-'0')
			} else {
				whole = whole*10 + float64(r-'0')
			}
		}
	}
	val := whole + frac/fracDiv
	if neg {
		val = -val
	}
	return val
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
