package model

import (
	"testing"
	"time"

	"ConvertGo/internal/config"
)

func validSpec() JobSpec {
	return JobSpec{Mode: JobModeIndividual, Inputs: []string{"input.mp4"}, OutputPath: "out.mp4", Profile: config.Profile{ID: "p", Name: "P", Container: "mp4", VideoEncoder: "libx264", AudioEncoder: "aac"}, SubtitleMode: SubtitleModeNone}
}

func TestValidateJobSpecDefaultsAndInputCounts(t *testing.T) {
	spec := validSpec()
	spec.Mode = ""
	spec.HardwareDecode = ""
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.ApplyDefaults()
	if spec.Mode != JobModeIndividual || spec.HardwareDecode != "auto" || spec.SubtitleMode != SubtitleModeNone {
		t.Fatalf("defaults = mode:%q hardware:%q subtitle:%q", spec.Mode, spec.HardwareDecode, spec.SubtitleMode)
	}
	spec = validSpec()
	spec.Inputs = []string{"one.mp4", "two.mp4"}
	if err := spec.Validate(); err == nil {
		t.Fatal("individual mode accepted multiple inputs")
	}
	spec = validSpec()
	spec.Mode = JobModeConcatenate
	if err := spec.Validate(); err == nil {
		t.Fatal("concatenate mode accepted fewer than two inputs")
	}
	spec.Inputs = []string{"one.mp4", "two.mp4"}
	if err := spec.Validate(); err != nil {
		t.Fatalf("valid concatenate spec rejected: %v", err)
	}
}

func TestValidateJobSpecRejectsInvalidTrimCropAndScale(t *testing.T) {
	spec := validSpec()
	spec.TrimStart, spec.TrimEnd = 5*time.Second, 4*time.Second
	if err := spec.Validate(); err == nil {
		t.Fatal("reversed trim range accepted")
	}
	spec = validSpec()
	spec.Crop = &CropRect{X: 0, Y: 0, Width: 101, Height: 100}
	if err := spec.Validate(); err == nil {
		t.Fatal("odd crop width accepted")
	}
	spec = validSpec()
	spec.Width = 320
	if err := spec.Validate(); err == nil {
		t.Fatal("only one output dimension accepted")
	}
}

func TestValidateJobSpecRequiresSubtitleStreamForSubtitleModes(t *testing.T) {
	spec := validSpec()
	spec.SubtitleMode = SubtitleModeCopy
	if err := spec.Validate(); err == nil {
		t.Fatal("subtitle copy without a stream accepted")
	}
	stream := 2
	spec.SubtitleStream = &stream
	if err := spec.Validate(); err != nil {
		t.Fatalf("subtitle copy with selected stream rejected: %v", err)
	}
}

func TestEncoderOptionsRejectReservedKeys(t *testing.T) {
	for _, key := range []string{"i", "map", "progress", "y", "filter_complex", "output", "protocol_whitelist"} {
		spec := validSpec()
		spec.EncoderOptions = map[string]string{key: "x"}
		if err := spec.Validate(); err == nil {
			t.Errorf("reserved option key %q accepted", key)
		}
	}
}

func TestValidateJobSpecRejectsInvalidHardwareDecodeAndInput(t *testing.T) {
	spec := validSpec()
	spec.HardwareDecode = "backend:"
	if err := spec.Validate(); err == nil {
		t.Fatal("empty hardware backend accepted")
	}
	spec = validSpec()
	spec.Inputs[0] = "\x00"
	if err := spec.Validate(); err == nil {
		t.Fatal("NUL-containing input accepted")
	}
}
