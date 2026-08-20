// Package server exposes health/status information and generated media files.
package server

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"github.com/sam1314/ffmpeg-go-streamer/internal/config"
	"github.com/sam1314/ffmpeg-go-streamer/internal/player"
)

type Server struct {
	http   *http.Server
	player *player.Player
	mode   config.StreamMode
}

func New(addr, outputDir string, mode config.StreamMode, p *player.Player) *Server {
	s := &Server{player: p, mode: mode}
	mux := http.NewServeMux()
	mux.Handle("/media/", http.StripPrefix("/media/", http.FileServer(http.Dir(outputDir))))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/api/status", s.status)
	mux.HandleFunc("/", s.home)
	s.http = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return s
}

func (s *Server) ListenAndServe() error { return s.http.ListenAndServe() }

func (s *Server) Shutdown() error {
	ctx, cancel := contextWithTimeout(5 * time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	state := s.player.Snapshot()
	_ = json.NewEncoder(w).Encode(struct {
		Playing     bool   `json:"playing"`
		Preparing   bool   `json:"preparing"`
		Title       string `json:"title,omitempty"`
		Artist      string `json:"artist,omitempty"`
		AudioSource string `json:"audio_source,omitempty"`
		Queue       int    `json:"queue_length"`
		LastOutput  string `json:"last_output,omitempty"`
		LastError   string `json:"last_error,omitempty"`
	}{state.Playing, state.Preparing, state.Now.Title, state.Now.Artist, state.AudioSource, len(state.Queue), state.LastOutput, state.LastError})
}

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Live Music Video</title><style>
body{margin:0;background:#16171a;color:#fff;font:16px system-ui,sans-serif;display:grid;min-height:100vh;place-items:center}
main{width:min(1100px,92vw)} video,img{width:100%;border-radius:18px;box-shadow:0 18px 60px #0008;background:#333}
p{color:#b9bbc2} code{color:#fff} .badge{display:inline-block;padding:.3rem .65rem;border-radius:99px;background:#2c2f36}
</style></head><body><main><h1>Live Music Video</h1>
{{if .HLS}}<video controls autoplay playsinline poster="/media/idle.png"><source src="/media/hls/stream.m3u8" type="application/vnd.apple.mpegurl"></video>{{else}}<img src="/media/idle.png" alt="Idle music player">{{end}}
<p><span class="badge" id="state">Ready</span> Use <code>/search</code> in the Telegram bot to choose a song.</p>
<script>async function tick(){try{let s=await(await fetch('/api/status')).json();document.querySelector('#state').textContent=s.preparing?'Preparing '+s.title:s.playing?'Playing '+s.title:'Ready';}catch(e){}}setInterval(tick,2000);tick()</script>
</main></body></html>`))

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = homeTemplate.Execute(w, struct{ HLS bool }{s.mode == config.StreamHLS})
}

// Kept as a tiny wrapper so tests can replace time behavior without exporting
// server internals.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
