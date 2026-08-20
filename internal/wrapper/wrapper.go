// Package wrapper is a typed client for the wrapper-manager gRPC service
// (WorldObservationLog/wrapper-manager), which exposes Apple Music audio decrypt
// (M3U8 -> License -> Decrypt) and synced TTML lyrics keyed by adam_id.
package wrapper

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/sam1314/ffmpeg-go-streamer/internal/wrapper/pb"
)

// Client wraps a gRPC connection to wrapper-manager.
type Client struct {
	conn *grpc.ClientConn
	rpc  pb.WrapperManagerServiceClient
	addr string
}

// Status is a snapshot of the wrapper's readiness.
type Status struct {
	OK          bool
	Ready       bool
	Regions     []string
	ClientCount int
}

// Dial connects to wrapper-manager at addr (host:port). The connection is lazy;
// it does not block waiting for the server.
func Dial(addr string) (*Client, error) {
	return DialTLS(addr, false)
}

// DialTLS connects to wrapper-manager and optionally enables transport TLS.
// Public wrapper-manager deployments normally use TLS; a local Docker service
// normally does not.
func DialTLS(addr string, secure bool) (*Client, error) {
	if addr == "" {
		return nil, fmt.Errorf("empty wrapper address")
	}
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "https://"), "http://")
	creds := credentials.TransportCredentials(insecure.NewCredentials())
	if secure {
		host := addr
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		creds = credentials.NewTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("dial wrapper %s: %w", addr, err)
	}
	return &Client{conn: conn, rpc: pb.NewWrapperManagerServiceClient(conn), addr: addr}, nil
}

// Addr returns the configured endpoint.
func (c *Client) Addr() string { return c.addr }

// Close closes the underlying connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Status queries the wrapper's health. A non-nil error means unreachable.
func (c *Client) Status(ctx context.Context) (Status, error) {
	reply, err := c.rpc.Status(ctx, &emptypb.Empty{})
	if err != nil {
		return Status{}, err
	}
	d := reply.GetData()
	return Status{
		OK:          d.GetStatus(),
		Ready:       d.GetReady(),
		Regions:     d.GetRegions(),
		ClientCount: int(d.GetClientCount()),
	}, nil
}

// Ready reports whether the wrapper is reachable and ready to serve within a
// short timeout. Errors are swallowed into false so callers can decide fallback.
func (c *Client) Ready(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	st, err := c.Status(cctx)
	if err != nil {
		return false
	}
	return st.OK && st.Ready
}

// M3U8 returns the HLS playlist string for a track.
func (c *Client) M3U8(ctx context.Context, adamID string) (string, error) {
	reply, err := c.rpc.M3U8(ctx, &pb.M3U8Request{Data: &pb.M3U8DataRequest{AdamId: adamID}})
	if err != nil {
		return "", err
	}
	if code := reply.GetHeader().GetCode(); code != 0 {
		return "", fmt.Errorf("wrapper M3U8 code=%d msg=%q", code, reply.GetHeader().GetMsg())
	}
	return reply.GetData().GetM3U8(), nil
}

// WebPlayback returns the web-playback HLS/master playlist for a track.
func (c *Client) WebPlayback(ctx context.Context, adamID string) (string, error) {
	reply, err := c.rpc.WebPlayback(ctx, &pb.WebPlaybackRequest{Data: &pb.WebPlaybackDataRequest{AdamId: adamID}})
	if err != nil {
		return "", err
	}
	if code := reply.GetHeader().GetCode(); code != 0 {
		return "", fmt.Errorf("wrapper WebPlayback code=%d msg=%q", code, reply.GetHeader().GetMsg())
	}
	return reply.GetData().GetM3U8(), nil
}

// LyricsTTML returns the raw syllable-TTML lyrics for a track. region/language
// may be empty to use the wrapper's defaults.
func (c *Client) LyricsTTML(ctx context.Context, adamID, region, language string) (string, error) {
	reply, err := c.rpc.Lyrics(ctx, &pb.LyricsRequest{Data: &pb.LyricsDataRequest{
		AdamId:   adamID,
		Region:   region,
		Language: language,
	}})
	if err != nil {
		return "", err
	}
	if code := reply.GetHeader().GetCode(); code != 0 {
		return "", fmt.Errorf("wrapper Lyrics code=%d msg=%q", code, reply.GetHeader().GetMsg())
	}
	return reply.GetData().GetLyrics(), nil
}

// License requests a FairPlay/HLS content license for the given key URI and
// challenge (base64). Returned license is base64.
func (c *Client) License(ctx context.Context, adamID, challenge, uri string) (license string, renew int64, err error) {
	reply, err := c.rpc.License(ctx, &pb.LicenseRequest{Data: &pb.LicenseDataRequest{
		AdamId:    adamID,
		Challenge: challenge,
		Uri:       uri,
	}})
	if err != nil {
		return "", 0, err
	}
	if code := reply.GetHeader().GetCode(); code != 0 {
		return "", 0, fmt.Errorf("wrapper License code=%d msg=%q", code, reply.GetHeader().GetMsg())
	}
	return reply.GetData().GetLicense(), reply.GetData().GetRenew(), nil
}

// Sample is one encrypted media sample handed to the decrypt stream.
type Sample struct {
	Index int
	Key   string // per-track decryption key (base64/hex as the wrapper expects)
	Data  []byte
}

// DecryptStream opens the bidirectional Decrypt stream and returns a helper that
// sends encrypted samples and reads the decrypted results back, preserving
// order by sample index. The caller must call Close when done.
type DecryptStream struct {
	stream grpc.BidiStreamingClient[pb.DecryptRequest, pb.DecryptReply]
	adamID string
}

// OpenDecrypt starts a Decrypt stream for a track.
func (c *Client) OpenDecrypt(ctx context.Context, adamID string) (*DecryptStream, error) {
	s, err := c.rpc.Decrypt(ctx)
	if err != nil {
		return nil, err
	}
	return &DecryptStream{stream: s, adamID: adamID}, nil
}

// Send submits one encrypted sample for decryption.
func (d *DecryptStream) Send(s Sample) error {
	return d.stream.Send(&pb.DecryptRequest{Data: &pb.DecryptData{
		AdamId:      d.adamID,
		Key:         s.Key,
		SampleIndex: int32(s.Index),
		Sample:      s.Data,
	}})
}

// Recv reads one decrypted sample back.
func (d *DecryptStream) Recv() (Sample, error) {
	reply, err := d.stream.Recv()
	if err != nil {
		return Sample{}, err
	}
	if code := reply.GetHeader().GetCode(); code != 0 {
		return Sample{}, fmt.Errorf("wrapper Decrypt code=%d msg=%q", code, reply.GetHeader().GetMsg())
	}
	data := reply.GetData()
	return Sample{Index: int(data.GetSampleIndex()), Key: data.GetKey(), Data: data.GetSample()}, nil
}

// CloseSend signals no more samples will be sent.
func (d *DecryptStream) CloseSend() error { return d.stream.CloseSend() }
