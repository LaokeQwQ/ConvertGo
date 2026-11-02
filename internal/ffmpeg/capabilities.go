package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

type CodecCapability struct {
	Name     string `json:"name"`
	Codec    string `json:"codec"`
	Hardware bool   `json:"hardware"`
	Backend  string `json:"backend"`
}

type Capabilities struct {
	Containers       []string            `json:"containers"`
	VideoEncoders    []CodecCapability   `json:"videoEncoders"`
	AudioEncoders    []CodecCapability   `json:"audioEncoders"`
	HardwareDecoders []CodecCapability   `json:"hardwareDecoders"`
	SubtitleCopy     map[string][]string `json:"subtitleCopy"`
	SubtitleBurn     []string            `json:"subtitleBurn"`
	ConcatSubtitles  bool                `json:"concatSubtitles"`
}

var codecName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func (e *Engine) Capabilities(ctx context.Context) (Capabilities, error) {
	if e == nil || strings.TrimSpace(e.ffmpegPath) == "" {
		return Capabilities{}, errors.New("ffmpeg executable path is required")
	}
	if ctx == nil {
		return Capabilities{}, errors.New("context is required")
	}
	encoders, err := e.query(ctx, "-encoders")
	if err != nil {
		return Capabilities{}, err
	}
	muxers, err := e.query(ctx, "-muxers")
	if err != nil {
		return Capabilities{}, err
	}
	decoders, err := e.query(ctx, "-decoders")
	if err != nil {
		return Capabilities{}, err
	}
	filters, err := e.query(ctx, "-filters")
	if err != nil {
		return Capabilities{}, err
	}
	caps := Capabilities{
		VideoEncoders: make([]CodecCapability, 0), AudioEncoders: make([]CodecCapability, 0),
		HardwareDecoders: make([]CodecCapability, 0), SubtitleCopy: make(map[string][]string), SubtitleBurn: make([]string, 0),
	}
	caps.Containers = parseMuxers(muxers)
	for _, name := range parseCodecNames(encoders) {
		entry := classifyCodec(name)
		if encoderIsVideo(encoders, name) {
			if !entry.Hardware || e.runtimeEncoder(ctx, entry) {
				caps.VideoEncoders = append(caps.VideoEncoders, entry)
			}
		} else {
			caps.AudioEncoders = append(caps.AudioEncoders, entry)
		}
	}
	for _, name := range parseCodecNames(decoders) {
		entry := classifyCodec(name)
		if entry.Hardware && e.runtimeHardware(ctx, entry.Backend) {
			caps.HardwareDecoders = append(caps.HardwareDecoders, entry)
		}
	}
	if containsFilter(filters, "subtitles") {
		caps.SubtitleBurn = []string{"ass", "ssa", "subrip", "webvtt", "mov_text"}
	}
	available := make(map[string]bool, len(caps.Containers))
	for _, name := range caps.Containers {
		available[name] = true
	}
	if available["mp4"] {
		caps.SubtitleCopy["mp4"] = []string{"mov_text"}
	}
	if available["mov"] {
		caps.SubtitleCopy["mov"] = []string{"mov_text"}
	}
	if available["webm"] {
		caps.SubtitleCopy["webm"] = []string{"webvtt"}
	}
	if available["matroska"] {
		caps.SubtitleCopy["matroska"] = []string{"ass", "ssa", "subrip", "webvtt", "mov_text", "dvd_subtitle", "hdmv_pgs_subtitle"}
	}
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	return caps, nil
}

func (e *Engine) query(ctx context.Context, flag string) (string, error) {
	cmd := exec.CommandContext(ctx, e.ffmpegPath, "-hide_banner", flag)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("query ffmpeg %s: %w: %s", flag, err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func parseNames(output string, column int) []string {
	seen := make(map[string]struct{})
	var names []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) <= column || !strings.Contains(fields[0], ".") || !codecName.MatchString(fields[column]) {
			continue
		}
		if _, ok := seen[fields[column]]; ok {
			continue
		}
		seen[fields[column]] = struct{}{}
		names = append(names, fields[column])
	}
	return names
}

func parseMuxers(output string) []string {
	seen := make(map[string]struct{})
	var names []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(fields[0], "E") || !codecName.MatchString(fields[1]) {
			continue
		}
		if _, ok := seen[fields[1]]; ok {
			continue
		}
		seen[fields[1]] = struct{}{}
		names = append(names, fields[1])
	}
	return names
}

func parseCodecNames(output string) []string { return parseNames(output, 1) }

func encoderIsVideo(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[1] == name && strings.HasPrefix(fields[0], "V") {
			return true
		}
	}
	return false
}

func classifyCodec(name string) CodecCapability {
	entry := CodecCapability{Name: name, Codec: name}
	for _, suffix := range []struct{ suffix, backend string }{
		{"_videotoolbox", "videotoolbox"}, {"_nvenc", "nvenc"}, {"_qsv", "qsv"}, {"_amf", "amf"}, {"_vaapi", "vaapi"},
	} {
		if strings.HasSuffix(name, suffix.suffix) {
			entry.Hardware = true
			entry.Backend = suffix.backend
			entry.Codec = strings.TrimSuffix(name, suffix.suffix)
			break
		}
	}
	return entry
}

func (e *Engine) runtimeEncoder(ctx context.Context, codec CodecCapability) bool {
	if !codec.Hardware {
		return true
	}
	args := []string{"-hide_banner", "-f", "lavfi", "-i", "color=c=black:s=16x16:r=1", "-frames:v", "1", "-c:v", codec.Name, "-f", "null", "-"}
	return e.runProbe(ctx, args)
}

func (e *Engine) runtimeHardware(ctx context.Context, backend string) bool {
	device := backend
	if backend == "nvenc" {
		device = "cuda"
	}
	args := []string{"-hide_banner", "-init_hw_device", device + "=convertgo", "-f", "lavfi", "-i", "color=c=black:s=16x16:r=1", "-frames:v", "1", "-f", "null", "-"}
	return e.runProbe(ctx, args)
}

func (e *Engine) runProbe(ctx context.Context, args []string) bool {
	cmd := exec.CommandContext(ctx, e.ffmpegPath, args...)
	if err := cmd.Run(); err != nil {
		return false
	}
	return ctx.Err() == nil
}

func containsFilter(output, filter string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == filter {
			return true
		}
	}
	return false
}
