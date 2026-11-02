package model

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ConvertGo/internal/config"
)

type JobMode string

const (
	JobModeIndividual  JobMode = "individual"
	JobModeConcatenate JobMode = "concatenate"
)

type SubtitleMode string

const (
	SubtitleModeNone SubtitleMode = "none"
	SubtitleModeCopy SubtitleMode = "copy"
	SubtitleModeBurn SubtitleMode = "burn"
)

type JobState string

const (
	JobQueued    JobState = "queued"
	JobProbing   JobState = "probing"
	JobRunning   JobState = "running"
	JobCompleted JobState = "completed"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

type CropRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type JobSpec struct {
	Inputs             []string          `json:"inputs"`
	OutputPath         string            `json:"outputPath"`
	Mode               JobMode           `json:"mode"`
	Profile            config.Profile    `json:"profile"`
	VideoStream        *int              `json:"videoStream,omitempty"`
	AudioStreams       []int             `json:"audioStreams"`
	SubtitleStream     *int              `json:"subtitleStream,omitempty"`
	SubtitleMode       SubtitleMode      `json:"subtitleMode"`
	TrimStart          time.Duration     `json:"trimStart"`
	TrimEnd            time.Duration     `json:"trimEnd"`
	Crop               *CropRect         `json:"crop,omitempty"`
	Width              int               `json:"width"`
	Height             int               `json:"height"`
	FrameRate          string            `json:"frameRate"`
	Deinterlace        bool              `json:"deinterlace"`
	HardwareDecode     string            `json:"hardwareDecode"`
	EncoderOptions     map[string]string `json:"encoderOptions"`
	OverwriteConfirmed bool              `json:"overwriteConfirmed"`
}

func (s *JobSpec) ApplyDefaults() {
	if s.Mode == "" {
		s.Mode = JobModeIndividual
	}
	if s.SubtitleMode == "" {
		s.SubtitleMode = SubtitleModeNone
	}
	if s.HardwareDecode == "" {
		s.HardwareDecode = "auto"
	}
}

func (s JobSpec) Validate() error {
	s.ApplyDefaults()
	if s.Mode != JobModeIndividual && s.Mode != JobModeConcatenate {
		return fmt.Errorf("unsupported job mode %q", s.Mode)
	}
	if len(s.Inputs) == 0 || (s.Mode == JobModeIndividual && len(s.Inputs) != 1) || (s.Mode == JobModeConcatenate && len(s.Inputs) < 2) {
		return errors.New("individual jobs require one input; concatenate jobs require at least two")
	}
	for i, input := range s.Inputs {
		if strings.TrimSpace(input) == "" || strings.IndexByte(input, 0) >= 0 {
			return fmt.Errorf("input %d is empty or contains NUL", i)
		}
	}
	if strings.TrimSpace(s.OutputPath) == "" || strings.IndexByte(s.OutputPath, 0) >= 0 {
		return errors.New("output path is required")
	}
	if s.TrimStart < 0 || s.TrimEnd < 0 || (s.TrimEnd != 0 && s.TrimEnd <= s.TrimStart) {
		return errors.New("trim end must be zero or greater than trim start")
	}
	if s.Crop != nil && (s.Crop.X < 0 || s.Crop.Y < 0 || s.Crop.Width <= 0 || s.Crop.Height <= 0 || s.Crop.Width%2 != 0 || s.Crop.Height%2 != 0) {
		return errors.New("crop requires nonnegative offsets and positive even dimensions")
	}
	if s.Width < 0 || s.Height < 0 || (s.Width == 0) != (s.Height == 0) {
		return errors.New("output width and height must both be zero or positive")
	}
	if s.FrameRate != "" && !validFrameRate(s.FrameRate) {
		return errors.New("invalid frame rate")
	}
	if s.VideoStream != nil && *s.VideoStream < 0 || s.SubtitleStream != nil && *s.SubtitleStream < 0 {
		return errors.New("stream indexes must be nonnegative")
	}
	for _, index := range s.AudioStreams {
		if index < 0 {
			return errors.New("audio stream indexes must be nonnegative")
		}
	}
	if s.HardwareDecode != "off" && s.HardwareDecode != "auto" && !strings.HasPrefix(s.HardwareDecode, "backend:") {
		return errors.New("hardware decode must be off, auto, or backend:<name>")
	}
	if strings.HasPrefix(s.HardwareDecode, "backend:") && strings.TrimSpace(strings.TrimPrefix(s.HardwareDecode, "backend:")) == "" {
		return errors.New("hardware backend name is required")
	}
	if s.SubtitleMode != SubtitleModeNone && s.SubtitleMode != SubtitleModeCopy && s.SubtitleMode != SubtitleModeBurn {
		return fmt.Errorf("unsupported subtitle mode %q", s.SubtitleMode)
	}
	if s.SubtitleMode != SubtitleModeNone && s.SubtitleStream == nil {
		return errors.New("subtitle stream is required for copy or burn mode")
	}
	if err := s.Profile.Validate(); err != nil {
		return err
	}
	for _, options := range []map[string]string{s.Profile.VideoOptions, s.Profile.AudioOptions, s.EncoderOptions} {
		for key, value := range options {
			if strings.TrimSpace(key) == "" || strings.IndexByte(key, 0) >= 0 || strings.IndexByte(value, 0) >= 0 {
				return errors.New("encoder options require nonempty keys and NUL-free values")
			}
			if isReservedEncoderOption(key) {
				return fmt.Errorf("encoder option %q is reserved", key)
			}
		}
	}
	return nil
}

func isReservedEncoderOption(key string) bool {
	k := strings.ToLower(strings.TrimLeft(key, "-"))
	for _, reserved := range []string{"i", "input", "map", "progress", "y", "n", "overwrite", "f", "format", "o", "output", "protocol", "protocols", "protocol_blacklist", "protocol_whitelist", "ss", "to", "t", "hwaccel", "init_hw_device", "filter_complex", "filter", "vf", "af", "c", "codec"} {
		if k == reserved || strings.HasPrefix(k, reserved+":") || strings.HasPrefix(k, "map_") || strings.HasPrefix(k, "filter_complex") {
			return true
		}
	}
	return false
}

func validFrameRate(rate string) bool {
	if strings.TrimSpace(rate) != rate {
		return false
	}
	for _, r := range rate {
		if !(r >= '0' && r <= '9' || r == '.' || r == '/') {
			return false
		}
	}
	parts := strings.Split(rate, "/")
	if len(parts) == 2 {
		numerator, nErr := strconv.ParseInt(parts[0], 10, 64)
		denominator, dErr := strconv.ParseInt(parts[1], 10, 64)
		return nErr == nil && dErr == nil && numerator > 0 && denominator > 0
	}
	if len(parts) != 1 {
		return false
	}
	value, err := strconv.ParseFloat(rate, 64)
	return err == nil && value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

type Progress struct {
	Frame         int64         `json:"frame"`
	OutTime       time.Duration `json:"outTime"`
	TotalDuration time.Duration `json:"totalDuration"`
	Speed         float64       `json:"speed"`
}

type Job struct {
	ID       string   `json:"id"`
	Spec     JobSpec  `json:"spec"`
	State    JobState `json:"state"`
	Progress Progress `json:"progress"`
	Error    string   `json:"error"`
}
