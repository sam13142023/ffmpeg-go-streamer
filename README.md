# FFmpeg Go Streamer

一个简洁的 Golang 库，用于封装 FFmpeg 功能，专注于 RTMPS 推流和音视频合并。

## 功能特性

- ✅ RTMPS 推流支持（带密钥验证）
- ✅ 图片和音频合并为视频流
- ✅ 实时推流功能
- ✅ 连接重试机制
- ✅ 简化的 API 设计
- ✅ 错误处理和日志记录

## 安装

```bash
go get github.com/sam13142023/ffmpeg-go-streamer
```

## 前置要求

确保系统已安装 FFmpeg：

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install ffmpeg

# macOS
brew install ffmpeg

# Windows
# 下载 FFmpeg 并添加到 PATH
```

## 快速开始

### 基本 RTMPS 推流

```go
package main

import (
    "log"
    "time"
    
    ffmpeg "github.com/sam13142023/ffmpeg-go-streamer"
)

func main() {
    // 配置 RTMPS
    rtmpsConfig := &ffmpeg.RTMPSConfig{
        Server:    "rtmps://live.example.com/live",
        StreamKey: "your-stream-key-here",
        TLSVerify: true,
    }

    // 创建推流器
    streamer := ffmpeg.NewRTMPSStreamer(rtmpsConfig)

    // 推流选项
    options := &ffmpeg.StreamOptions{
        VideoCodec: "libx264",
        AudioCodec: "aac",
        Bitrate:    "2000k",
        FrameRate:  25,
        Resolution: "1920x1080",
        RetryCount: 3,
        RetryDelay: 5 * time.Second,
    }

    // 开始推流
    if err := streamer.StreamToRTMPS("input.mp4", options); err != nil {
        log.Fatal(err)
    }
}
```

### 音视频合并

```go
// 创建合并器
merger := ffmpeg.NewMerger()

// 合并配置
config := &ffmpeg.MergeConfig{
    ImageDuration: 30.0,           // 30 秒
    ImageScale:    "1920:1080",    // Full HD
    AudioLoop:     true,           // 循环播放音频
}

// 合并图片和音频
err := merger.MergeImageAndAudio(
    "background.jpg", 
    "music.mp3", 
    "output.mp4", 
    config,
)
```

### 合并并推流

```go
// 直接合并音视频并推流到 RTMPS
err := merger.MergeAndStreamToRTMPS(
    "background.jpg",
    "music.mp3", 
    rtmpsConfig,
    mergeConfig,
    streamOptions,
)
```

## API 文档

### Streamer

主要的推流器结构，提供基础的 FFmpeg 功能。

```go
streamer := ffmpeg.NewStreamer()
streamer.SetFFmpegPath("/path/to/ffmpeg")  // 可选：设置 FFmpeg 路径
streamer.SetTimeout(30 * time.Second)      // 可选：设置超时时间
```

### RTMPSConfig

RTMPS 推流配置结构：

```go
type RTMPSConfig struct {
    Server     string            // RTMPS 服务器地址
    StreamKey  string            // 推流密钥
    Username   string            // 用户名（可选）
    Password   string            // 密码（可选）
    TLSVerify  bool              // 是否验证 TLS 证书
    ExtraArgs  map[string]string // 额外的 FFmpeg 参数
}
```

### StreamOptions

推流选项配置：

```go
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
```

### MergeConfig

音视频合并配置：

```go
type MergeConfig struct {
    ImageDuration float64           // 图片显示时长（秒）
    ImageScale    string            // 图片缩放，默认 "1920:1080"
    AudioLoop     bool              // 是否循环音频
    OutputFormat  string            // 输出格式，默认 "mp4"
    ExtraParams   map[string]string // 额外参数
}
```

## 使用示例

更多详细示例请参考 `examples/` 目录：

- `basic_streaming.go` - 基础推流示例
- `advanced_config.go` - 高级配置示例
- `live_streaming.go` - 实时推流示例

## 错误处理

库提供了详细的错误信息和自动重试机制：

```go
options := &ffmpeg.StreamOptions{
    RetryCount: 5,                    // 最多重试 5 次
    RetryDelay: 3 * time.Second,      // 重试间隔 3 秒
}

if err := streamer.StreamToRTMPS("input.mp4", options); err != nil {
    // 处理错误
    log.Printf("推流失败: %v", err)
}
```

## 支持的格式

### 输入格式
- **视频**: MP4, AVI, MOV, MKV, FLV
- **音频**: MP3, WAV, AAC, OGG, FLAC
- **图片**: JPEG, PNG, BMP, TIFF

### 输出格式
- **推流**: RTMPS/RTMP (FLV)
- **文件**: MP4, AVI, MOV, MKV

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！

## 联系

如有问题，请创建 Issue 或联系项目维护者。