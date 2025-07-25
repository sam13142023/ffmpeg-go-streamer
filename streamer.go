package ffmpeg

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "time"

    "github.com/pkg/errors"
)

// Streamer 主要的推流器结构
type Streamer struct {
    ffmpegPath string
    timeout    time.Duration
}

// StreamOptions 推流选项
type StreamOptions struct {
    VideoCodec   string            // 视频编码器，默认 libx264
    AudioCodec   string            // 音频编码器，默认 aac
    Bitrate      string            // 码率，默认 2000k
    FrameRate    int               // 帧率，默认 25
    Resolution   string            // 分辨率，默认 1920x1080
    ExtraParams  map[string]string // 额外参数
    RetryCount   int               // 重试次数，默认 3
    RetryDelay   time.Duration     // 重试延迟，默认 5s
}

// NewStreamer 创建新的推流器
func NewStreamer() *Streamer {
    return &Streamer{
        ffmpegPath: "ffmpeg", // 假设 ffmpeg 在 PATH 中
        timeout:    30 * time.Second,
    }
}

// SetFFmpegPath 设置 FFmpeg 可执行文件路径
func (s *Streamer) SetFFmpegPath(path string) {
    s.ffmpegPath = path
}

// SetTimeout 设置命令超时时间
func (s *Streamer) SetTimeout(timeout time.Duration) {
    s.timeout = timeout
}

// getDefaultOptions 获取默认选项
func getDefaultOptions() *StreamOptions {
    return &StreamOptions{
        VideoCodec:  "libx264",
        AudioCodec:  "aac",
        Bitrate:     "2000k",
        FrameRate:   25,
        Resolution:  "1920x1080",
        ExtraParams: make(map[string]string),
        RetryCount:  3,
        RetryDelay:  5 * time.Second,
    }
}

// checkFFmpeg 检查 FFmpeg 是否可用
func (s *Streamer) checkFFmpeg() error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    cmd := exec.CommandContext(ctx, s.ffmpegPath, "-version")
    if err := cmd.Run(); err != nil {
        return errors.Wrap(err, "FFmpeg not found or not working")
    }
    return nil
}

// buildCommand 构建 FFmpeg 命令
func (s *Streamer) buildCommand(input, output string, options *StreamOptions) []string {
    if options == nil {
        options = getDefaultOptions()
    }

    args := []string{
        "-y",                    // 覆盖输出文件
        "-i", input,             // 输入文件/流
        "-c:v", options.VideoCodec,
        "-c:a", options.AudioCodec,
        "-b:v", options.Bitrate,
        "-r", fmt.Sprintf("%d", options.FrameRate),
        "-s", options.Resolution,
        "-f", "flv",             // RTMP 需要 FLV 格式
    }

    // 添加额外参数
    for key, value := range options.ExtraParams {
        args = append(args, key, value)
    }

    args = append(args, output)
    return args
}

// StreamFile 推流文件到 RTMPS
func (s *Streamer) StreamFile(inputFile, rtmpsURL string, options *StreamOptions) error {
    if err := s.checkFFmpeg(); err != nil {
        return err
    }

    if _, err := os.Stat(inputFile); os.IsNotExist(err) {
        return errors.New("input file does not exist")
    }

    if options == nil {
        options = getDefaultOptions()
    }

    var lastErr error
    for attempt := 0; attempt <= options.RetryCount; attempt++ {
        if attempt > 0 {
            fmt.Printf("Retrying stream attempt %d/%d...\n", attempt, options.RetryCount)
            time.Sleep(options.RetryDelay)
        }

        args := s.buildCommand(inputFile, rtmpsURL, options)
        
        ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
        cmd := exec.CommandContext(ctx, s.ffmpegPath, args...)
        
        if err := cmd.Run(); err != nil {
            lastErr = errors.Wrapf(err, "streaming attempt %d failed", attempt+1)
            cancel()
            continue
        }
        
        cancel()
        return nil
    }

    return errors.Wrap(lastErr, "all streaming attempts failed")
}