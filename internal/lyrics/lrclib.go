package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

var ErrNotFound = errors.New("lyrics not found")

// LRCLibProvider fetches synced LRC (or plain lyrics as a last resort) from
// LRCLIB's public API. The API requires an identifying User-Agent and asks
// clients to avoid burst traffic, so requests are serialized and cached.
type LRCLibProvider struct {
	baseURL string
	client  *http.Client

	mu          sync.Mutex
	lastRequest time.Time
	cache       map[string]model.Lyrics
}

type lrcLibRecord struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

func NewLRCLibProvider(baseURL string) *LRCLibProvider {
	return &LRCLibProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 6 * time.Second},
		cache:   make(map[string]model.Lyrics),
	}
}

func (p *LRCLibProvider) Get(ctx context.Context, track model.Track) (model.Lyrics, error) {
	if strings.TrimSpace(track.Title) == "" || strings.TrimSpace(track.Artist) == "" {
		return model.Lyrics{}, fmt.Errorf("%w: missing title or artist", ErrNotFound)
	}
	key := track.AdamID + "\x00" + track.Title + "\x00" + track.Artist
	p.mu.Lock()
	if cached, ok := p.cache[key]; ok {
		p.mu.Unlock()
		return cached, nil
	}
	p.mu.Unlock()

	record, err := p.exact(ctx, track)
	if errors.Is(err, ErrNotFound) {
		// Exact matching can miss alternate album names. Search once by title and
		// artist, then choose the closest duration match.
		if waitErr := p.waitBetweenRequests(ctx); waitErr != nil {
			return model.Lyrics{}, waitErr
		}
		record, err = p.search(ctx, track)
	}
	if err != nil {
		return model.Lyrics{}, err
	}

	var result model.Lyrics
	switch {
	case record.Instrumental:
		result = model.Lyrics{}
	case strings.TrimSpace(record.SyncedLyrics) != "":
		result, err = ParseLRC(record.SyncedLyrics, track.DurationMS)
	case strings.TrimSpace(record.PlainLyrics) != "":
		result = plainText(record.PlainLyrics, "")
	default:
		err = ErrNotFound
	}
	if err != nil {
		return model.Lyrics{}, err
	}
	p.mu.Lock()
	p.cache[key] = result
	p.mu.Unlock()
	return result, nil
}

func (p *LRCLibProvider) exact(ctx context.Context, track model.Track) (lrcLibRecord, error) {
	q := url.Values{}
	q.Set("track_name", track.Title)
	q.Set("artist_name", track.Artist)
	if track.Album != "" {
		q.Set("album_name", track.Album)
	}
	if track.DurationMS > 0 {
		q.Set("duration", strconv.Itoa((track.DurationMS+500)/1000))
	}
	var record lrcLibRecord
	err := p.getJSON(ctx, p.baseURL+"/api/get?"+q.Encode(), &record)
	return record, err
}

func (p *LRCLibProvider) search(ctx context.Context, track model.Track) (lrcLibRecord, error) {
	q := url.Values{}
	q.Set("track_name", track.Title)
	q.Set("artist_name", track.Artist)
	var records []lrcLibRecord
	if err := p.getJSON(ctx, p.baseURL+"/api/search?"+q.Encode(), &records); err != nil {
		return lrcLibRecord{}, err
	}
	best := -1
	bestScore := int(^uint(0) >> 1)
	for i, candidate := range records {
		if !equalFoldLoose(candidate.TrackName, track.Title) || !equalFoldLoose(candidate.ArtistName, track.Artist) {
			continue
		}
		diff := 0
		if track.DurationMS > 0 && candidate.Duration > 0 {
			diff = abs(int(candidate.Duration*1000) - track.DurationMS)
		}
		if diff < bestScore {
			best, bestScore = i, diff
		}
	}
	if best < 0 {
		return lrcLibRecord{}, ErrNotFound
	}
	return records[best], nil
}

func (p *LRCLibProvider) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ffmpeg-go-streamer/1.0 (https://github.com/sam1314/ffmpeg-go-streamer)")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("LRCLIB request: %w", err)
	}
	defer resp.Body.Close()
	defer func() {
		p.mu.Lock()
		p.lastRequest = time.Now()
		p.mu.Unlock()
	}()
	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode LRCLIB response: %w", err)
		}
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusTooManyRequests:
		return fmt.Errorf("LRCLIB rate limited (retry after %s)", resp.Header.Get("Retry-After"))
	default:
		return fmt.Errorf("LRCLIB HTTP %d", resp.StatusCode)
	}
}

func (p *LRCLibProvider) waitBetweenRequests(ctx context.Context) error {
	p.mu.Lock()
	wait := 250*time.Millisecond - time.Since(p.lastRequest)
	p.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func equalFoldLoose(a, b string) bool {
	normalize := func(s string) string {
		return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
	}
	return normalize(a) == normalize(b)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
