package ffmpeg_go_streamer

import (
    "fmt"
    "net/url"
    "strings"
    "github.com/pkg/errors"
)

type RTMPSConfig struct {
    Server     string            // RTMPS 服务器地址
    StreamKey  string            // 推流密钥
    Username   string            // 用户名（如果需要）
    Password   string            // 密码（如果需要）
    TLSVerify  bool              // 是否验证 TLS 证书
    ExtraArgs  map[string]string // 额外的 FFmpeg 参数
}

// RTMPSStreamer RTMPS 推流器
type RTMPSStreamer struct {
    *Streamer
    config *RTMPSConfig
}

// NewRTMPSStreamer 创建 RTMPS 推流器
func NewRTMPSStreamer(config *RTMPSConfig) *RTMPSStreamer {
    return &RTMPSStreamer{
        Streamer: NewStreamer(),
        config:   config,
    }
}

// buildRTMPSURL 构建 RTMPS URL
func (r *RTMPSStreamer) buildRTMPSURL() (string, error) {
    if r.config.Server == "" {
        return "", errors.New("RTMPS server is required")
    }

    // 确保使用 rtmps 协议
    server := r.config.Server
    if !strings.HasPrefix(server, "rtmps://") {
        if strings.HasPrefix(server, "rtmp://") {
            server = strings.Replace(server, "rtmp://", "rtmps://", 1)
        } else {
            server = "rtmps://" + server
        }
    }

    // 解析 URL
    u, err := url.Parse(server)
    if err != nil {
        return "", errors.Wrap(err, "invalid RTMPS server URL")
    }

    // 添加认证信息
    if r.config.Username != "" && r.config.Password != "" {
        u.User = url.UserPassword(r.config.Username, r.config.Password)
    }

    // 添加推流密钥
    if r.config.StreamKey != "" {
        if !strings.HasSuffix(u.Path, "/") {
            u.Path += "/"
        }
        u.Path += r.config.StreamKey
    }

    return u.String(), nil
}

// StreamToRTMPS 推流到 RTMPS
func (r *RTMPSStreamer) StreamToRTMPS(inputFile string, options *StreamOptions) error {
    rtmpsURL, err := r.buildRTMPSURL()
    if err != nil {
        return err
    }

    if options == nil {
        options = getDefaultOptions()
    }

    // 添加 RTMPS 特定参数
    if r.config.ExtraArgs != nil {
        for key, value := range r.config.ExtraArgs {
            options.ExtraParams[key] = value
        }
    }

    // TLS 验证设置
    if !r.config.TLSVerify {
        options.ExtraParams["-rtmp_conn"] = "S:0"
    }

    fmt.Printf("Streaming to RTMPS: %s\n", rtmpsURL)
    return r.StreamFile(inputFile, rtmpsURL, options)
}

// TestConnection 测试 RTMPS 连接
func (r *RTMPSStreamer) TestConnection() error {
    rtmpsURL, err := r.buildRTMPSURL()
    if err != nil {
        return err
    }

    // 创建一个简单的测试流
    args := []string{
        "-f", "lavfi",
        "-i", "testsrc=duration=1:size=320x240:rate=1",
        "-f", "flv",
        "-t", "1",
        rtmpsURL,
    }

    return r.runFFmpegCommand(args)
}