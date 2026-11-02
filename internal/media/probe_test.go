package media

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeRejectsRemoteAndNonRegularInputs(t *testing.T) {
	for _, input := range []string{"https://example.test/video.mp4", "rtmp://example.test/live"} {
		if _, err := Probe(context.Background(), "ffprobe", input); err == nil || !strings.Contains(err.Error(), "local") {
			t.Errorf("Probe(%q) error = %v, want local-only rejection", input, err)
		}
	}
	dir := t.TempDir()
	if _, err := Probe(context.Background(), "ffprobe", dir); err == nil {
		t.Fatal("directory accepted as media input")
	}
}

func TestParseProbeOutputPreservesSupportedStreamMetadata(t *testing.T) {
	data := []byte(`{"format":{"format_name":"mov,mp4","duration":"12.5"},"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"r_frame_rate":"30000/1001","tags":{"language":"und"}},{"index":1,"codec_type":"audio","codec_name":"aac","sample_rate":"48000","channels":2,"tags":{"language":"en"}},{"index":2,"codec_type":"subtitle","codec_name":"mov_text"},{"index":3,"codec_type":"attachment","codec_name":"ttf"}]}`)
	info, err := parseProbeOutput(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.Container != "mov,mp4" || info.DurationSeconds != 12.5 || len(info.Streams) != 3 {
		t.Fatalf("parsed probe info = %+v", info)
	}
	if got := info.Streams[0]; got.Type != "video" || got.Width != 1920 || got.FrameRate != "30000/1001" {
		t.Fatalf("video stream = %+v", got)
	}
	if got := info.Streams[1]; got.Type != "audio" || got.SampleRate != "48000" || got.Language != "en" {
		t.Fatalf("audio stream = %+v", got)
	}
}

func TestProbeDoesNotInvokeFFprobeForMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.mp4")
	if _, err := Probe(context.Background(), "ffprobe", path); err == nil {
		t.Fatal("missing input accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing input unexpectedly created: %v", err)
	}
}

func TestParseProbeOutputRejectsMalformedDuration(t *testing.T) {
	_, err := parseProbeOutput([]byte(`{"format":{"duration":"bad"}}`))
	if err == nil {
		t.Fatal("invalid duration accepted")
	}
}
