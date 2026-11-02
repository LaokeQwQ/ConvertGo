package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ConvertGo/internal/config"
	"ConvertGo/internal/model"
)

func argsSpec() model.JobSpec {
	return model.JobSpec{Mode: model.JobModeIndividual, Inputs: []string{"/tmp/source file;$(not-a-command).mp4"}, OutputPath: "/tmp/output file.mp4", Profile: config.Profile{ID: "p", Name: "P", Container: "mp4", VideoEncoder: "libx264", AudioEncoder: "aac"}}
}

func TestBuildArgsKeepsPathsAsSingleArguments(t *testing.T) {
	spec := argsSpec()
	spec.Crop = &model.CropRect{X: 0, Y: 0, Width: 320, Height: 180}
	spec.Width, spec.Height, spec.FrameRate = 640, 360, "30000/1001"
	spec.Profile.VideoOptions = map[string]string{"crf": "20"}
	spec.Profile.AudioOptions = map[string]string{"b:a": "128k"}
	spec.EncoderOptions = map[string]string{"preset": "fast"}
	args, err := BuildArgs(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "-i", spec.Inputs[0]) || args[len(args)-1] != spec.OutputPath {
		t.Fatalf("paths were not preserved as argv values: %#v", args)
	}
	if strings.Contains(strings.Join(args, " "), "$(not-a-command)") == false {
		t.Fatalf("input path missing from argv: %#v", args)
	}
	if !containsPair(args, "-vf", "crop=320:180:0:0,scale=640:360,fps=30000/1001") {
		t.Fatalf("video filters missing from argv: %#v", args)
	}
	if !containsPair(args, "-crf:v", "20") || !containsPair(args, "-b:a", "128k") || !containsPair(args, "-preset", "fast") {
		t.Fatalf("encoder options received incorrect stream specifiers: %#v", args)
	}
}

func TestBuildArgsRejectsURLAndUsesSelectedStreams(t *testing.T) {
	spec := argsSpec()
	spec.Inputs[0] = "https://example.test/movie.mp4"
	if _, err := BuildArgs(spec); err == nil {
		t.Fatal("URL input accepted")
	}
	spec = argsSpec()
	video, audio := 2, 3
	spec.VideoStream, spec.AudioStreams = &video, []int{audio}
	args, err := BuildArgs(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !containsPair(args, "-map", "0:2") || !containsPair(args, "-map", "0:3") {
		t.Fatalf("selected stream maps missing: %#v", args)
	}
}

func TestBuildArgsBuildsConcatFilter(t *testing.T) {
	spec := argsSpec()
	spec.Mode = model.JobModeConcatenate
	spec.Inputs = []string{"first.mp4", "second.mp4"}
	spec.AudioStreams = []int{1}
	args, err := BuildArgs(spec)
	if err != nil {
		t.Fatal(err)
	}
	graph := pairValue(args, "-filter_complex")
	if !strings.Contains(graph, "concat=n=2:v=1:a=1") || !containsPair(args, "-map", "[vout]") || !containsPair(args, "-map", "[aout0]") {
		t.Fatalf("invalid concat graph/outputs: %q %#v", graph, args)
	}
}

func TestBuildArgsRejectsConcatSubtitleCopyAndBurn(t *testing.T) {
	for _, mode := range []model.SubtitleMode{model.SubtitleModeCopy, model.SubtitleModeBurn} {
		spec := argsSpec()
		spec.Mode = model.JobModeConcatenate
		spec.Inputs = []string{"first.mp4", "second.mp4"}
		spec.SubtitleMode = mode
		subtitle := 2
		spec.SubtitleStream = &subtitle
		if _, err := BuildArgs(spec); err == nil || !strings.Contains(err.Error(), "unavailable in concatenate mode") {
			t.Errorf("mode %q error = %v, want explicit unsupported-mode error", mode, err)
		}
	}
}
func TestConsumeProgressParsesFramesTimeAndSpeed(t *testing.T) {
	var got []model.Progress
	data := "frame=27\nfps=24.0\nout_time_ms=1250000\nout_time=00:00:01.250000\nspeed=1.25x\nprogress=continue\nframe=30\nout_time=01:02:03.500000\nspeed=2x\nprogress=end\n"
	if err := consumeProgress(strings.NewReader(data), func(p model.Progress) { got = append(got, p) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Frame != 27 || got[0].OutTime != 1250*time.Millisecond || got[0].Speed != 1.25 {
		t.Fatalf("first progress = %+v", got)
	}
	if got[1].OutTime != time.Hour+2*time.Minute+3500*time.Millisecond || got[1].Speed != 2 {
		t.Fatalf("long progress = %+v", got[1])
	}
}

func TestEngineRunCancelsChildProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell helper uses a POSIX script")
	}
	path := filepath.Join(t.TempDir(), "fake-ffmpeg")
	script := "#!/bin/sh\nprintf 'frame=1\\nout_time=00:00:01.000000\\nprogress=continue\\n'\nexec sleep 10\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	engine := NewEngine(path, "")
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx, nil, nil) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("Run error = %v, want cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ffmpeg child did not exit after context cancellation")
	}
}

func TestCapabilityParsersClassifyHardwareEncoders(t *testing.T) {
	output := " V..... h264_videotoolbox H.264\n V..... libx264 H.264\n A..... aac AAC\n"
	names := parseCodecNames(output)
	if len(names) != 3 {
		t.Fatalf("encoder names = %#v", names)
	}
	got := classifyCodec(names[0])
	if !got.Hardware || got.Backend != "videotoolbox" || got.Codec != "h264" {
		t.Fatalf("hardware encoder classification = %+v", got)
	}
	if !encoderIsVideo(output, "libx264") || encoderIsVideo(output, "aac") {
		t.Fatal("encoder media-type parsing is incorrect")
	}
	if got := parseMuxers(" E mp4 MPEG-4 Part 14\n DE matroska Matroska\n D. avi AVI demuxer only\n"); len(got) != 2 || got[0] != "mp4" || got[1] != "matroska" {
		t.Fatalf("container parser result = %#v", got)
	}
	if !containsFilter(" ... subtitles V->V Subtitle rendering", "subtitles") {
		t.Fatal("subtitle filter was not detected")
	}
}

func TestEngineCapabilitiesDiscoversContainersEncodersAndSubtitles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX script")
	}
	script := "#!/bin/sh\ncase \"$2\" in\n-encoders) printf ' V..... libx264 H.264\\n A..... aac AAC\\n' ;;\n-muxers) printf ' E mp4 MPEG-4\\n E webm WebM\\n' ;;\n-decoders) printf ' V..... h264 H.264\\n' ;;\n-filters) printf ' ... subtitles V->V Subtitle overlay\\n' ;;\nesac\n"
	path := filepath.Join(t.TempDir(), "fake-ffmpeg")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	caps, err := NewEngine(path, "").Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(caps.Containers) != 2 || len(caps.VideoEncoders) != 1 || caps.VideoEncoders[0].Name != "libx264" || len(caps.AudioEncoders) != 1 || caps.AudioEncoders[0].Name != "aac" {
		t.Fatalf("discovered capabilities = %+v", caps)
	}
	if len(caps.SubtitleBurn) == 0 || len(caps.SubtitleCopy["mp4"]) != 1 || len(caps.SubtitleCopy["webm"]) != 1 {
		t.Fatalf("subtitle capabilities = %+v", caps)
	}
	if caps.ConcatSubtitles {
		t.Fatal("concat subtitle support reported although the concat filter cannot carry subtitles")
	}
}

func containsPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func pairValue(args []string, key string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}
