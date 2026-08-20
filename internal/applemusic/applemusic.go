// Package applemusic queries Apple's public iTunes Search API for song
// metadata. wrapper-manager has no search RPC, so search, artwork and preview
// URLs come from here; the resulting trackId doubles as the Apple Music adam_id
// used by every wrapper-manager RPC.
package applemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sam1314/ffmpeg-go-streamer/internal/model"
)

const (
	searchEndpoint = "https://itunes.apple.com/search"
	lookupEndpoint = "https://itunes.apple.com/lookup"
)

// Client is a thin iTunes Search API client.
type Client struct {
	http       *http.Client
	storefront string
}

// New returns a Client for the given storefront/country (e.g. "us").
func New(storefront string) *Client {
	if storefront == "" {
		storefront = "us"
	}
	return &Client{
		http:       &http.Client{Timeout: 15 * time.Second},
		storefront: strings.ToLower(storefront),
	}
}

// itunesResult mirrors the fields we use from an iTunes Search API entry.
type itunesResult struct {
	TrackID         int64  `json:"trackId"`
	TrackName       string `json:"trackName"`
	ArtistName      string `json:"artistName"`
	CollectionName  string `json:"collectionName"`
	ArtworkURL100   string `json:"artworkUrl100"`
	PreviewURL      string `json:"previewUrl"`
	TrackTimeMillis int    `json:"trackTimeMillis"`
	Country         string `json:"country"`
	Kind            string `json:"kind"`
	WrapperType     string `json:"wrapperType"`
}

type itunesResponse struct {
	ResultCount int            `json:"resultCount"`
	Results     []itunesResult `json:"results"`
}

// Search returns up to limit song results for term, skipping the first offset
// results (used for pagination). iTunes caps limit at 200; offset is emulated by
// over-fetching and slicing since the API has no true offset parameter.
func (c *Client) Search(ctx context.Context, term string, limit, offset int) ([]model.Track, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, fmt.Errorf("empty search term")
	}
	if limit <= 0 {
		limit = 5
	}
	// iTunes has no offset param; fetch enough to cover offset+limit then slice.
	fetch := offset + limit
	if fetch > 200 {
		fetch = 200
	}

	q := url.Values{}
	q.Set("term", term)
	q.Set("entity", "song")
	q.Set("media", "music")
	q.Set("limit", strconv.Itoa(fetch))
	q.Set("country", c.storefront)

	resp, err := c.get(ctx, searchEndpoint+"?"+q.Encode())
	if err != nil {
		return nil, err
	}

	tracks := make([]model.Track, 0, len(resp.Results))
	for _, r := range resp.Results {
		tracks = append(tracks, c.toTrack(r))
	}
	if offset >= len(tracks) {
		return nil, nil
	}
	end := offset + limit
	if end > len(tracks) {
		end = len(tracks)
	}
	return tracks[offset:end], nil
}

// Lookup fetches a single track by adam_id (iTunes trackId).
func (c *Client) Lookup(ctx context.Context, adamID string) (*model.Track, error) {
	q := url.Values{}
	q.Set("id", adamID)
	q.Set("country", c.storefront)
	q.Set("entity", "song")

	resp, err := c.get(ctx, lookupEndpoint+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	for _, r := range resp.Results {
		if r.TrackID != 0 {
			t := c.toTrack(r)
			return &t, nil
		}
	}
	return nil, fmt.Errorf("track %s not found", adamID)
}

func (c *Client) get(ctx context.Context, u string) (*itunesResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ffmpeg-go-streamer/1.0")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("itunes request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("itunes status %d", res.StatusCode)
	}
	var out itunesResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode itunes response: %w", err)
	}
	return &out, nil
}

func (c *Client) toTrack(r itunesResult) model.Track {
	return model.Track{
		AdamID:     strconv.FormatInt(r.TrackID, 10),
		Title:      r.TrackName,
		Artist:     r.ArtistName,
		Album:      r.CollectionName,
		ArtworkURL: artworkTemplate(r.ArtworkURL100),
		PreviewURL: r.PreviewURL,
		DurationMS: r.TrackTimeMillis,
		Region:     regionOf(r, c.storefront),
	}
}

func regionOf(r itunesResult, fallback string) string {
	if r.Country != "" {
		// iTunes returns ISO-3166 alpha-3 (e.g. "USA"); wrapper wants alpha-2.
		if a2, ok := iso3to2[strings.ToUpper(r.Country)]; ok {
			return a2
		}
	}
	return fallback
}

// artworkTemplate converts a fixed-size artwork URL like
// ".../100x100bb.jpg" into a "{w}x{h}bb.jpg" template so callers can request an
// arbitrary resolution.
func artworkTemplate(u string) string {
	if u == "" {
		return ""
	}
	// Replace the last "<n>x<n>bb.jpg" (or similar) segment with a template.
	idx := strings.LastIndex(u, "/")
	if idx < 0 {
		return u
	}
	prefix, last := u[:idx+1], u[idx+1:]
	// last looks like "100x100bb.jpg"; swap the leading NxN with {w}x{h}.
	if xi := strings.Index(last, "x"); xi > 0 {
		// find end of the second number
		rest := last[xi+1:]
		j := 0
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		suffix := rest[j:] // e.g. "bb.jpg"
		return prefix + "{w}x{h}" + suffix
	}
	return u
}
