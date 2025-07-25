package main

import (
	"fmt"
	"log"
	"time"

	ffmpeg "github.com/sam13142023/ffmpeg-go-streamer"
)

func main() {
    // 基本 RTMPS 推流示例
    basicRTMPSExample()
    
    // 音视频合并示例
    mergeExample()
    
    // 合并并推流示例
    mergeAndStreamExample()
}

func basicRTMPSExample() {
    fmt.Println("=== Basic RTMPS Streaming Example ===")
    
    // 配置 RTMPS
    rtmpsConfig := &ffmpeg.RTMPSConfig{
        Server:    "rtmps://live.example.com/live",
        StreamKey: "your-stream-key-here",
        TLSVerify: true,
    }

    // 创建 RTMPS 推流器
    streamer := ffmpeg.NewRTMPSStreamer(rtmpsConfig)

    // 配置推流选项
    options := &ffmpeg.StreamOptions{
        VideoCodec: "libx264",
        AudioCodec: "aac",
        Bitrate:    "2000k",
        FrameRate:  25,
        Resolution: "1920x1080",
        RetryCount: 3,
        RetryDelay: 5 * time.Second,
    }

    // 推流视频文件
    inputFile := "sample.mp4"
    if err := streamer.StreamToRTMPS(inputFile, options); err != nil {
        log.Printf("Streaming failed: %v", err)
    } else {
        fmt.Println("Streaming completed successfully!")
    }
}

func mergeExample() {
    fmt.Println("\n=== Merge Image and Audio Example ===")
    
    // 创建合并器
    merger := ffmpeg.NewMerger()

    // 配置合并选项
    config := &ffmpeg.MergeConfig{
		ImageDuration: 30.0,        // 30 秒
		ImageScale:    "1920:1080", // Full HD
		AudioLoop:     true,        // 循环播放音频
        OutputFormat:  "mp4",
    }

    // 合并图片和音频
    imagePath := "background.jpg"
    audioPath := "background_music.mp3"
    outputPath := "output_video.mp4"

    if err := merger.MergeImageAndAudio(imagePath, audioPath, outputPath, config); err != nil {
        log.Printf("Merge failed: %v", err)
    } else {
        fmt.Println("Merge completed successfully!")
    }
}

func mergeAndStreamExample() {
    fmt.Println("\n=== Merge and Stream Example ===")
    
    // 创建合并器
    merger := ffmpeg.NewMerger()

    // RTMPS 配置
    rtmpsConfig := &ffmpeg.RTMPSConfig{
        Server:    "rtmps://live.example.com/live",
        StreamKey: "your-stream-key-here",
        TLSVerify: true,
    }

    // 合并配置
    mergeConfig := &ffmpeg.MergeConfig{
        ImageDuration: 60.0,        // 1 分钟
        ImageScale:    "1920x1080",
        AudioLoop:     true,
    }

    // 推流配置
    streamOptions := &ffmpeg.StreamOptions{
        VideoCodec: "libx264",
        AudioCodec: "aac",
        Bitrate:    "3000k",
        FrameRate:  30,
        Resolution: "1920x1080",
        RetryCount: 5,
        RetryDelay: 3 * time.Second,
    }

    // 直接合并并推流
    imagePath := "live_background.jpg"
    audioPath := "live_audio.mp3"

    if err := merger.MergeAndStreamToRTMPS(imagePath, audioPath, rtmpsConfig, mergeConfig, streamOptions); err != nil {
        log.Printf("Merge and stream failed: %v", err)
    } else {
        fmt.Println("Merge and stream completed successfully!")
    }
}