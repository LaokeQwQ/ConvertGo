package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ConvertGo/internal/media"
	"ConvertGo/internal/model"
)

type Engine struct {
	ffmpegPath  string
	ffprobePath string
}

func NewEngine(ffmpegPath, ffprobePath string) *Engine {
	return &Engine{ffmpegPath: ffmpegPath, ffprobePath: ffprobePath}
}

func (e *Engine) FFprobePath() string {
	if e == nil {
		return ""
	}
	return e.ffprobePath
}

func (e *Engine) Probe(ctx context.Context, inputPath string) (media.MediaInfo, error) {
	if e == nil {
		return media.MediaInfo{}, errors.New("ffmpeg engine is nil")
	}
	return media.Probe(ctx, e.ffprobePath, inputPath)
}

func BuildArgs(spec model.JobSpec) ([]string, error) {
	spec.ApplyDefaults()
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if err := media.ValidateLocalPath(spec.OutputPath); err != nil {
		return nil, fmt.Errorf("output path: %w", err)
	}
	if err := validateOutputPath(spec.OutputPath); err != nil {
		return nil, err
	}
	args := []string{"-hide_banner", "-nostdin", "-progress", "pipe:1", "-nostats"}
	if strings.HasPrefix(spec.HardwareDecode, "backend:") {
		args = append(args, "-hwaccel", strings.TrimPrefix(spec.HardwareDecode, "backend:"))
	} else if spec.HardwareDecode == "auto" {
		args = append(args, "-hwaccel", "auto")
	}
	for _, input := range spec.Inputs {
		if err := media.ValidateLocalPath(input); err != nil {
			return nil, err
		}
		if spec.TrimStart > 0 {
			args = append(args, "-ss", seconds(spec.TrimStart))
		}
		if spec.TrimEnd > 0 {
			args = append(args, "-to", seconds(spec.TrimEnd))
		}
		args = append(args, "-i", input)
	}
	if spec.Mode == model.JobModeConcatenate {
		if spec.SubtitleMode != model.SubtitleModeNone {
			return nil, errors.New("subtitle copy and burn-in are unavailable in concatenate mode")
		}
		graph, outputs, err := concatFilter(spec)
		if err != nil {
			return nil, err
		}
		args = append(args, "-filter_complex", graph)
		for _, output := range outputs {
			args = append(args, "-map", output)
		}
	} else {
		appendIndividualMaps(&args, spec)
		filters, err := videoFilters(spec, spec.Inputs[0])
		if err != nil {
			return nil, err
		}
		if len(filters) != 0 {
			args = append(args, "-vf", strings.Join(filters, ","))
		}
	}
	args = appendCodecOptions(args, "v", spec.Profile.VideoEncoder, spec.Profile.VideoOptions)
	args = appendCodecOptions(args, "a", spec.Profile.AudioEncoder, spec.Profile.AudioOptions)
	args = appendEncoderOptions(args, spec.EncoderOptions)
	args = append(args, "-f", spec.Profile.Container, spec.OutputPath)
	return args, nil
}

func appendIndividualMaps(args *[]string, spec model.JobSpec) {
	if spec.VideoStream == nil {
		*args = append(*args, "-map", "0:v:0?")
	} else {
		*args = append(*args, "-map", fmt.Sprintf("0:%d", *spec.VideoStream))
	}
	if len(spec.AudioStreams) == 0 {
		*args = append(*args, "-map", "0:a?")
	} else {
		for _, index := range spec.AudioStreams {
			if index < 0 {
				return
			}
			*args = append(*args, "-map", fmt.Sprintf("0:%d", index))
		}
	}
	if spec.SubtitleMode == model.SubtitleModeCopy && spec.SubtitleStream != nil {
		*args = append(*args, "-map", fmt.Sprintf("0:%d", *spec.SubtitleStream), "-c:s", "copy")
	}
}

func concatFilter(spec model.JobSpec) (string, []string, error) {
	video := true
	if spec.VideoStream != nil && *spec.VideoStream < 0 {
		video = false
	}
	audio := spec.AudioStreams
	if len(audio) == 0 {
		audio = []int{-1}
	}
	if !video && len(audio) == 0 {
		return "", nil, errors.New("concatenate requires at least one selected stream")
	}
	filters := make([]string, 0, len(spec.Inputs)*2+1)
	concatInputs := make([]string, 0, len(spec.Inputs)*(len(audio)+1))
	outputs := make([]string, 0, len(audio)+1)
	for i := range spec.Inputs {
		if video {
			stream := "v:0"
			if spec.VideoStream != nil {
				stream = strconv.Itoa(*spec.VideoStream)
			}
			chain, err := videoFilters(spec, spec.Inputs[i])
			if err != nil {
				return "", nil, err
			}
			prefix := fmt.Sprintf("[%d:%s]", i, stream)
			label := fmt.Sprintf("v%d", i)
			if len(chain) != 0 {
				filters = append(filters, prefix+strings.Join(chain, ",")+"["+label+"]")
			} else {
				filters = append(filters, prefix+"null["+label+"]")
			}
			concatInputs = append(concatInputs, "["+label+"]")
		}
		for j, stream := range audio {
			selector := "a:0"
			if stream >= 0 {
				selector = strconv.Itoa(stream)
			}
			label := fmt.Sprintf("a%d_%d", i, j)
			filters = append(filters, fmt.Sprintf("[%d:%s]anull[%s]", i, selector, label))
			concatInputs = append(concatInputs, "["+label+"]")
		}
	}
	if len(concatInputs) == 0 {
		return "", nil, errors.New("concatenate requires at least one selected stream")
	}
	outLabels := make([]string, 0, len(audio)+1)
	if video {
		outLabels = append(outLabels, "vout")
		outputs = append(outputs, "[vout]")
	}
	for j := range audio {
		label := fmt.Sprintf("aout%d", j)
		outLabels = append(outLabels, label)
		outputs = append(outputs, "["+label+"]")
	}
	filters = append(filters, strings.Join(concatInputs, "")+fmt.Sprintf("concat=n=%d:v=%d:a=%d", len(spec.Inputs), boolInt(video), len(audio))+"["+strings.Join(outLabels, "][")+"]")
	return strings.Join(filters, ";"), outputs, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func videoFilters(spec model.JobSpec, input string) ([]string, error) {
	filters := make([]string, 0, 5)
	if spec.Deinterlace {
		filters = append(filters, "yadif")
	}
	if spec.Crop != nil {
		filters = append(filters, fmt.Sprintf("crop=%d:%d:%d:%d", spec.Crop.Width, spec.Crop.Height, spec.Crop.X, spec.Crop.Y))
	}
	if spec.Width > 0 {
		filters = append(filters, fmt.Sprintf("scale=%d:%d", spec.Width, spec.Height))
	}
	if spec.FrameRate != "" {
		filters = append(filters, "fps="+spec.FrameRate)
	}
	if spec.SubtitleMode == model.SubtitleModeBurn && spec.SubtitleStream != nil {
		filters = append(filters, fmt.Sprintf("subtitles=filename='%s':si=%d", escapeFilterPath(input), *spec.SubtitleStream))
	}
	return filters, nil
}

func escapeFilterPath(path string) string {
	var b strings.Builder
	for _, r := range path {
		if strings.ContainsRune("\\':,[];", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func appendCodecOptions(args []string, stream, encoder string, options map[string]string) []string {
	if encoder != "" {
		args = append(args, "-c:"+stream, encoder)
	}
	return appendEncoderOptionsForStream(args, stream, options)
}

func appendEncoderOptions(args []string, options map[string]string) []string {
	return appendEncoderOptionsForStream(args, "", options)
}

func appendEncoderOptionsForStream(args []string, stream string, options map[string]string) []string {
	originalKeys := make([]string, 0, len(options))
	for key := range options {
		originalKeys = append(originalKeys, key)
	}
	sort.Strings(originalKeys)
	for _, original := range originalKeys {
		key := strings.TrimLeft(original, "-")
		flag := "-" + key
		if stream != "" && !strings.Contains(key, ":") {
			flag += ":" + stream
		}
		args = append(args, flag, options[original])
	}
	return args
}

func seconds(duration time.Duration) string {
	return strconv.FormatFloat(duration.Seconds(), 'f', -1, 64)
}

func (e *Engine) Run(ctx context.Context, args []string, emit func(model.Progress)) error {
	if e == nil || strings.TrimSpace(e.ffmpegPath) == "" {
		return errors.New("ffmpeg executable path is required")
	}
	if ctx == nil {
		return errors.New("context is required")
	}
	cmd := exec.CommandContext(ctx, e.ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open ffmpeg progress stream: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	progressErr := consumeProgress(stdout, emit)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if progressErr != nil {
		return fmt.Errorf("read ffmpeg progress: %w", progressErr)
	}
	if waitErr != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func consumeProgress(r io.Reader, emit func(model.Progress)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	current := model.Progress{}
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		switch key {
		case "frame":
			current.Frame, _ = strconv.ParseInt(value, 10, 64)
		case "out_time_ms":
			if micros, err := strconv.ParseInt(value, 10, 64); err == nil && micros >= 0 {
				current.OutTime = time.Duration(micros) * time.Microsecond
			}
		case "out_time":
			if duration, err := parseFFmpegTime(value); err == nil {
				current.OutTime = duration
			}
		case "speed":
			if speed, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(value), "x"), 64); err == nil && speed >= 0 {
				current.Speed = speed
			}
		case "progress":
			if emit != nil {
				emit(current)
			}
			current = model.Progress{}
		}
	}
	return scanner.Err()
}

func parseFFmpegTime(value string) (time.Duration, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid time %q", value)
	}
	hours, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || hours < 0 {
		return 0, fmt.Errorf("invalid time %q", value)
	}
	minutes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || minutes < 0 || minutes >= 60 {
		return 0, fmt.Errorf("invalid time %q", value)
	}
	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil || seconds < 0 || seconds >= 60 || math.IsInf(seconds, 0) || math.IsNaN(seconds) {
		return 0, fmt.Errorf("invalid time %q", value)
	}
	return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds*float64(time.Second)), nil
}

func validateOutputPath(path string) error {
	if strings.TrimSpace(path) == "" || strings.IndexByte(path, 0) >= 0 {
		return errors.New("output path is required")
	}
	if filepath.Clean(path) == "." {
		return errors.New("output path must name a file")
	}
	return nil
}
