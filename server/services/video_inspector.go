package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

type VideoMetadata struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

// GetTitle attempts to safely extract the title from tags (case-insensitive keys)
func (m *VideoMetadata) GetTitle() string {
	if m.Format.Tags == nil {
		return ""
	}
	// FFmpeg tags can be mixed case depending on container
	for k, v := range m.Format.Tags {
		if k == "title" || k == "TITLE" || k == "Title" {
			return v
		}
	}
	return ""
}

// GetQuality attempts to determine the resolution and codec from the video stream
func (m *VideoMetadata) GetQuality() string {
	for _, stream := range m.Streams {
		if stream.CodecType == "video" {
			res := ""
			if stream.Width >= 3800 {
				res = "4K"
			} else if stream.Width >= 1900 {
				res = "1080p"
			} else if stream.Width >= 1200 {
				res = "720p"
			} else if stream.Width > 0 {
				res = "SD"
			}

			codec := stream.CodecName
			if codec == "hevc" {
				codec = "HEVC"
			} else if codec == "h264" || codec == "avc" {
				codec = "H264"
			}

			if res != "" && codec != "" {
				return fmt.Sprintf("%s %s", res, codec)
			} else if res != "" {
				return res
			} else {
				return codec
			}
		}
	}
	return ""
}

// ProbeVideo uses ffprobe to extract metadata and stream information from a media file.
func ProbeVideo(ctx context.Context, filePath string) (*VideoMetadata, error) {
	// ffprobe can hang on broken or very large files over network shares, enforce 30s timeout
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Log string version of stderr to see actual ffprobe errors
		slog.Debug("ffprobe execution failed", "error", err, "stderr", stderr.String(), "file", filePath)
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var metadata VideoMetadata
	if err := json.Unmarshal(stdout.Bytes(), &metadata); err != nil {
		slog.Error("Failed to parse ffprobe json output", "error", err, "file", filePath)
		return nil, fmt.Errorf("json parse failed: %w", err)
	}

	return &metadata, nil
}
