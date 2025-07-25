package ffmpeg

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"

    "github.com/pkg/errors"
)

// MergeConfig 合并配置
type MergeConfig struct {
    ImageDuration float64           // 图片显示时长（秒）
    ImageScale    string            // 图片缩放，默认 "1920:1080"
    AudioLoop     bool              // 是否循环音频
    OutputFormat  string            // 输出格式，默认 "mp4"
    ExtraParams   map[string]string // 额外参数
}

// Merger 音视频合并器
type Merger struct {
    *Streamer
}

// NewMerger 创建合并器
func NewMerger() *Merger {
    return &Merger{
        Streamer: NewStreamer(),
    }
}

// getDefaultMergeConfig 获取默认合并配置
func getDefaultMergeConfig() *MergeConfig {
    return &MergeConfig{
        ImageDuration: 10.0,
        ImageScale:    "1920:1080",
        AudioLoop:     true,
        OutputFormat:  "mp4",
        ExtraParams:   make(map[string]string),
    }
}

// MergeImageAndAudio 合并图片和音频为视频
func (m *Merger) MergeImageAndAudio(imagePath, audioPath, outputPath string, config *MergeConfig) error {
    if err := m.checkFFmpeg(); err != nil {
        return err
    }

    // 检查输入文件
    if _, err := os.Stat(imagePath); os.IsNotExist(err) {
        return errors.New("image file does not exist")
    }
    if _, err := os.Stat(audioPath); os.IsNotExist(err) {
        return errors.New("audio file does not exist")
    }

    if config == nil {
        config = getDefaultMergeConfig()
    }

    // 创建输出目录
    if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
        return errors.Wrap(err, "failed to create output directory")
    }

    args := []string{
        "-y",
        "-loop", "1",
        "-i", imagePath,
        "-i", audioPath,
        "-c:v", "libx264",
        "-c:a", "aac",
        "-vf", fmt.Sprintf("scale=%s", config.ImageScale),
        "-shortest",
    }

    // 如果需要循环音频
    if config.AudioLoop {
        args = append(args, "-stream_loop", "-1")
    }

    // 设置图片持续时间
    if config.ImageDuration > 0 {
        args = append(args, "-t", fmt.Sprintf("%.2f", config.ImageDuration))
    }

    // 添加额外参数
    for key, value := range config.ExtraParams {
        args = append(args, key, value)
    }

    args = append(args, outputPath)

    return m.runFFmpegCommand(args)
}

// MergeAndStreamToRTMPS 合并音视频并直接推流到 RTMPS
func (m *Merger) MergeAndStreamToRTMPS(imagePath, audioPath string, rtmpsConfig *RTMPSConfig, mergeConfig *MergeConfig, streamOptions *StreamOptions) error {
    if err := m.checkFFmpeg(); err != nil {
        return err
    }

    // 检查输入文件
    if _, err := os.Stat(imagePath); os.IsNotExist(err) {
        return errors.New("image file does not exist")
    }
    if _, err := os.Stat(audioPath); os.IsNotExist(err) {
        return errors.New("audio file does not exist")
    }

    if mergeConfig == nil {
        mergeConfig = getDefaultMergeConfig()
    }
    if streamOptions == nil {
        streamOptions = getDefaultOptions()
    }

    // 构建 RTMPS URL
    rtmpsStreamer := NewRTMPSStreamer(rtmpsConfig)
    rtmpsURL, err := rtmpsStreamer.buildRTMPSURL()
    if err != nil {
        return err
    }

    args := []string{
        "-y",
        "-loop", "1",
        "-i", imagePath,
        "-i", audioPath,
        "-c:v", streamOptions.VideoCodec,
        "-c:a", streamOptions.AudioCodec,
        "-b:v", streamOptions.Bitrate,
        "-r", fmt.Sprintf("%d", streamOptions.FrameRate),
        "-vf", fmt.Sprintf("scale=%s", mergeConfig.ImageScale),
        "-f", "flv",
    }

    // 如果需要循环音频
    if mergeConfig.AudioLoop {
        args = append(args, "-stream_loop", "-1")
    }

    // 设置图片持续时间
    if mergeConfig.ImageDuration > 0 {
        args = append(args, "-t", fmt.Sprintf("%.2f", mergeConfig.ImageDuration))
    }

    // 添加额外参数
    for key, value := range mergeConfig.ExtraParams {
        args = append(args, key, value)
    }
    for key, value := range streamOptions.ExtraParams {
        args = append(args, key, value)
    }

    args = append(args, rtmpsURL)

    fmt.Printf("Merging and streaming to RTMPS: %s\n", rtmpsURL)
    return m.runFFmpegCommand(args)
}

// runFFmpegCommand 运行 FFmpeg 命令的通用方法
func (s *Streamer) runFFmpegCommand(args []string) error {
    ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
    defer cancel()

    cmd := exec.CommandContext(ctx, s.ffmpegPath, args...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    if err := cmd.Run(); err != nil {
        return errors.Wrap(err, "FFmpeg command failed")
    }

    return nil
}