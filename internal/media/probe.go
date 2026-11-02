package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type MediaInfo struct {
	Path            string       `json:"path"`
	Name            string       `json:"name"`
	Container       string       `json:"container"`
	DurationSeconds float64      `json:"durationSeconds"`
	SizeBytes       int64        `json:"sizeBytes"`
	Streams         []StreamInfo `json:"streams"`
}

type StreamInfo struct {
	Index      int    `json:"index"`
	Type       string `json:"type"`
	Codec      string `json:"codec"`
	Language   string `json:"language"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	FrameRate  string `json:"frameRate"`
	SampleRate string `json:"sampleRate"`
	Channels   int    `json:"channels"`
}

type probeOutput struct {
	Streams []struct {
		Index      int               `json:"index"`
		CodecType  string            `json:"codec_type"`
		CodecName  string            `json:"codec_name"`
		Width      int               `json:"width"`
		Height     int               `json:"height"`
		FrameRate  string            `json:"r_frame_rate"`
		SampleRate string            `json:"sample_rate"`
		Channels   int               `json:"channels"`
		Tags       map[string]string `json:"tags"`
	} `json:"streams"`
	Format struct {
		Name     string `json:"format_name"`
		Duration string `json:"duration"`
	} `json:"format"`
}

func Probe(ctx context.Context, ffprobePath, inputPath string) (MediaInfo, error) {
	if ctx == nil {
		return MediaInfo{}, errors.New("context is required")
	}
	if strings.TrimSpace(ffprobePath) == "" {
		return MediaInfo{}, errors.New("ffprobe path is required")
	}
	if err := rejectRemoteInput(inputPath); err != nil {
		return MediaInfo{}, err
	}
	info, err := os.Stat(inputPath)
	if err != nil {
		return MediaInfo{}, fmt.Errorf("stat input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return MediaInfo{}, errors.New("input must be a local regular file")
	}
	cmd := exec.CommandContext(ctx, ffprobePath, "-v", "error", "-show_entries", "format=format_name,duration:stream=index,codec_type,codec_name,width,height,r_frame_rate,sample_rate,channels:stream_tags=language", "-of", "json", inputPath)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return MediaInfo{}, ctx.Err()
		}
		return MediaInfo{}, fmt.Errorf("ffprobe input: %w", err)
	}
	parsed, err := parseProbeOutput(output)

	if err != nil {
		return MediaInfo{}, err
	}
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return MediaInfo{}, fmt.Errorf("resolve input path: %w", err)
	}
	parsed.Path, parsed.Name, parsed.SizeBytes = abs, filepath.Base(inputPath), info.Size()
	return parsed, nil
}

func rejectRemoteInput(inputPath string) error {
	if strings.TrimSpace(inputPath) == "" || strings.IndexByte(inputPath, 0) >= 0 {
		return errors.New("input path is required")
	}
	parsed, err := url.Parse(inputPath)
	if err == nil && parsed.Scheme != "" && !(len(parsed.Scheme) == 1 && len(inputPath) > 2 && inputPath[1] == ':') {
		return errors.New("only local file inputs are supported")
	}
	return nil
}

func parseProbeOutput(output []byte) (MediaInfo, error) {
	var raw probeOutput
	if err := json.Unmarshal(output, &raw); err != nil {
		return MediaInfo{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	info := MediaInfo{Container: raw.Format.Name}
	if raw.Format.Duration != "" {
		duration, err := strconv.ParseFloat(raw.Format.Duration, 64)
		if err != nil || duration < 0 || math.IsInf(duration, 0) || math.IsNaN(duration) {
			return MediaInfo{}, fmt.Errorf("invalid media duration %q", raw.Format.Duration)
		}
		info.DurationSeconds = duration
	}
	info.Streams = make([]StreamInfo, 0, len(raw.Streams))
	for _, stream := range raw.Streams {
		typ := stream.CodecType
		if typ != "video" && typ != "audio" && typ != "subtitle" && typ != "data" {
			continue
		}
		info.Streams = append(info.Streams, StreamInfo{
			Index: stream.Index, Type: typ, Codec: stream.CodecName,
			Language: stream.Tags["language"], Width: stream.Width, Height: stream.Height,
			FrameRate: stream.FrameRate, SampleRate: stream.SampleRate, Channels: stream.Channels,
		})
	}
	return info, nil
}

func ValidateLocalPath(inputPath string) error {
	return rejectRemoteInput(inputPath)
}
