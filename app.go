package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"ConvertGo/internal/config"
	"ConvertGo/internal/ffmpeg"
	"ConvertGo/internal/jobs"
	"ConvertGo/internal/media"
	"ConvertGo/internal/model"
	"ConvertGo/internal/platform"
	"ConvertGo/internal/update"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const sleepStatusEvent = "sleep:status"

// App is the local media and desktop boundary exposed to the Wails frontend.
type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	operationMu sync.Mutex
	settingsMu  sync.Mutex
	settings    config.Settings
	settingsErr error
	engine      *ffmpeg.Engine
	manager     *jobs.Manager

	sleepMu      sync.Mutex
	sleepJobs    map[string]bool
	sleepRelease func()
	sleepError   string
	sleepCancel  context.CancelFunc
}

func NewApp() *App {
	return &App{sleepJobs: make(map[string]bool)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx, a.cancel = context.WithCancel(ctx)
	settings, err := config.LoadSettings()
	a.settingsMu.Lock()
	a.settings, a.settingsErr = settings, err
	if err == nil {
		a.configureEngineLocked(settings)
	}
	a.settingsMu.Unlock()

	wailsruntime.OnFileDrop(ctx, func(_ int, _ int, paths []string) {
		if len(paths) > 0 {
			wailsruntime.EventsEmit(ctx, "files:dropped", paths)
		}
	})
}

func (a *App) shutdown(context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	if a.manager != nil {
		a.manager.Shutdown()
	}
	a.releaseSleepInhibitor()
	if a.ctx != nil {
		wailsruntime.OnFileDropOff(a.ctx)
	}
}

func (a *App) configureEngineLocked(settings config.Settings) {
	ffmpegPath, ffprobePath := bundledFFmpegPaths()
	if settings.FFmpegPath != "" {
		ffmpegPath = settings.FFmpegPath
		ffprobePath = filepath.Join(filepath.Dir(settings.FFmpegPath), executableName("ffprobe"))
	}
	engine := ffmpeg.NewEngine(ffmpegPath, ffprobePath)
	manager, err := jobs.NewManager(engine, settings.MaxConcurrentJobs, a.onJobChanged, a.onJobProgress)
	if err != nil {
		a.settingsErr = err
		return
	}
	a.engine = engine
	a.manager = manager
}

func bundledFFmpegPaths() (string, string) {
	executable, err := os.Executable()
	if err != nil {
		return executableName("ffmpeg"), executableName("ffprobe")
	}
	root := filepath.Join(filepath.Dir(executable), "resources", "ffmpeg", runtime.GOOS+"-"+runtime.GOARCH)
	return filepath.Join(root, executableName("ffmpeg")), filepath.Join(root, executableName("ffprobe"))
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func (a *App) runtimeComponents() (*ffmpeg.Engine, *jobs.Manager, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if a.settingsErr != nil {
		return nil, nil, a.settingsErr
	}
	if a.engine == nil || a.manager == nil {
		return nil, nil, errors.New("ConvertGo media engine is not initialized")
	}
	return a.engine, a.manager, nil
}

func (a *App) validRuntime() error {
	_, _, err := a.runtimeComponents()
	return err
}

func (a *App) ProbeFile(path string) (media.MediaInfo, error) {
	engine, _, err := a.runtimeComponents()
	if err != nil {
		return media.MediaInfo{}, err
	}
	return media.Probe(a.ctx, engine.FFprobePath(), path)
}

func (a *App) SelectFiles() ([]string, error) {
	if a.ctx == nil {
		return nil, errors.New("file picker is unavailable before application startup")
	}
	return wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:   "Add media files",
		Filters: []wailsruntime.FileFilter{{DisplayName: "Media files", Pattern: "*"}},
	})
}

func (a *App) SelectFFmpegPath() (string, error) {
	if a.ctx == nil {
		return "", errors.New("file picker is unavailable before application startup")
	}
	return wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{Title: "Select local FFmpeg executable"})
}

func (a *App) AddJob(spec model.JobSpec) (model.Job, error) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	_, manager, err := a.runtimeComponents()
	if err != nil {
		return model.Job{}, err
	}
	return manager.Add(spec)
}

func (a *App) GetJobs() []model.Job {
	a.settingsMu.Lock()
	manager := a.manager
	a.settingsMu.Unlock()
	if manager == nil {
		return []model.Job{}
	}
	return manager.GetJobs()
}

func (a *App) StartQueue() error {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	_, manager, err := a.runtimeComponents()
	if err != nil {
		return err
	}
	return manager.StartQueue()
}

func (a *App) CancelJob(jobID string) error {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	a.settingsMu.Lock()
	manager := a.manager
	a.settingsMu.Unlock()
	if manager == nil {
		return errors.New("ConvertGo media engine is not initialized")
	}
	return manager.Cancel(jobID)
}

func (a *App) RemoveJob(jobID string) error {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	a.settingsMu.Lock()
	manager := a.manager
	a.settingsMu.Unlock()
	if manager == nil {
		return errors.New("ConvertGo media engine is not initialized")
	}
	return manager.RemoveJob(jobID)
}

func (a *App) GetSettings() (config.Settings, error) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if a.settingsErr != nil {
		return config.Settings{}, a.settingsErr
	}
	return a.settings, nil
}

func (a *App) SaveSettings(settings config.Settings) error {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if err := settings.Validate(); err != nil {
		return err
	}
	if settings.MaxConcurrentJobs > runtime.NumCPU() {
		return fmt.Errorf("max_concurrent_jobs cannot exceed %d logical CPUs", runtime.NumCPU())
	}

	a.settingsMu.Lock()
	manager, oldEngine := a.manager, a.engine
	if manager == nil || oldEngine == nil {
		a.settingsMu.Unlock()
		return errors.New("media engine is unavailable while existing settings require repair")
	}
	old := a.settings
	engineChanged := settings.FFmpegPath != old.FFmpegPath
	newEngine := oldEngine
	if engineChanged {
		ffmpegPath, ffprobePath := bundledFFmpegPaths()
		if settings.FFmpegPath != "" {
			ffmpegPath = settings.FFmpegPath
			ffprobePath = filepath.Join(filepath.Dir(settings.FFmpegPath), executableName("ffprobe"))
		}
		newEngine = ffmpeg.NewEngine(ffmpegPath, ffprobePath)
		if _, err := newEngine.Capabilities(a.ctx); err != nil {
			a.settingsMu.Unlock()
			return fmt.Errorf("selected FFmpeg is not usable: %w", err)
		}
	}
	concurrencyChanged := settings.MaxConcurrentJobs != old.MaxConcurrentJobs
	if err := config.SaveSettings(settings); err != nil {
		a.settingsMu.Unlock()
		return err
	}
	if engineChanged {
		if err := manager.SetEngine(newEngine); err != nil {
			rollbackErr := config.SaveSettings(old)
			if rollbackErr != nil {
				a.settingsErr = fmt.Errorf("settings were saved but the FFmpeg engine could not change: %v; reverting settings also failed: %w", err, rollbackErr)
				a.settingsMu.Unlock()
				return a.settingsErr
			}
			a.settingsMu.Unlock()
			return err
		}
	}
	if concurrencyChanged {
		if err := manager.SetMaxConcurrent(settings.MaxConcurrentJobs); err != nil {
			var restoreErr error
			if engineChanged {
				restoreErr = manager.SetEngine(oldEngine)
			}
			rollbackErr := config.SaveSettings(old)
			if restoreErr != nil || rollbackErr != nil {
				a.settingsErr = fmt.Errorf("queue settings were only partially applied: concurrency: %v; restore engine: %v; restore config: %v", err, restoreErr, rollbackErr)
				a.settingsMu.Unlock()
				return a.settingsErr
			}
			a.settingsMu.Unlock()
			return err
		}
	}
	a.settings, a.settingsErr, a.engine = settings, nil, newEngine
	a.settingsMu.Unlock()
	a.updateSleepPreference(settings.PreventSleep)
	return nil
}

func (a *App) ListProfiles() ([]config.Profile, error) {
	engine, _, err := a.runtimeComponents()
	if err != nil {
		return nil, err
	}
	capabilities, err := engine.Capabilities(a.ctx)
	if err != nil {
		return nil, err
	}
	saved, err := config.ListProfiles()
	if err != nil {
		return nil, err
	}
	profiles := builtInProfiles(capabilities)
	seen := make(map[string]bool, len(profiles)+len(saved))
	for _, profile := range profiles {
		seen[profile.ID] = true
	}
	for _, profile := range saved {
		if seen[profile.ID] {
			continue
		}
		if profileSupported(profile, capabilities) {
			profiles = append(profiles, profile)
		}
	}
	return profiles, nil
}

func (a *App) SaveProfile(profile config.Profile) error {
	engine, _, err := a.runtimeComponents()
	if err != nil {
		return err
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	capabilities, err := engine.Capabilities(a.ctx)
	if err != nil {
		return err
	}
	if !profileSupported(profile, capabilities) {
		return errors.New("profile refers to a container or encoder unavailable in this FFmpeg build")
	}
	profiles, err := config.ListProfiles()
	if err != nil {
		return err
	}
	updated := make([]config.Profile, 0, len(profiles)+1)
	replaced := false
	for _, current := range profiles {
		if current.ID == profile.ID {
			updated = append(updated, profile)
			replaced = true
		} else {
			updated = append(updated, current)
		}
	}
	if !replaced {
		updated = append(updated, profile)
	}
	return config.SaveProfiles(updated)
}

func builtInProfiles(capabilities ffmpeg.Capabilities) []config.Profile {
	candidates := []config.Profile{
		{ID: "mp4-h264-aac", Name: "MP4 H.264 / AAC", Container: "mp4", VideoEncoder: "libx264", AudioEncoder: "aac", VideoOptions: map[string]string{"crf": "23"}, AudioOptions: map[string]string{"b:a": "192k"}},
		{ID: "mp4-hevc-aac", Name: "MP4 HEVC / AAC", Container: "mp4", VideoEncoder: "libx265", AudioEncoder: "aac", VideoOptions: map[string]string{"crf": "28"}, AudioOptions: map[string]string{"b:a": "192k"}},
		{ID: "webm-vp9-opus", Name: "WebM VP9 / Opus", Container: "webm", VideoEncoder: "libvpx-vp9", AudioEncoder: "libopus", VideoOptions: map[string]string{"crf": "32"}, AudioOptions: map[string]string{"b:a": "128k"}},
		{ID: "audio-mp3", Name: "MP3 audio", Container: "mp3", AudioEncoder: "libmp3lame", AudioOptions: map[string]string{"b:a": "192k"}},
		{ID: "audio-aac", Name: "AAC audio", Container: "adts", AudioEncoder: "aac", AudioOptions: map[string]string{"b:a": "192k"}},
		{ID: "audio-flac", Name: "FLAC audio", Container: "flac", AudioEncoder: "flac"},
	}
	profiles := make([]config.Profile, 0, len(candidates))
	for _, candidate := range candidates {
		if profileSupported(candidate, capabilities) {
			profiles = append(profiles, candidate)
		}
	}
	return profiles
}

func profileSupported(profile config.Profile, capabilities ffmpeg.Capabilities) bool {
	if !containsString(capabilities.Containers, profile.Container) {
		return false
	}
	if profile.VideoEncoder != "" && !containsEncoder(capabilities.VideoEncoders, profile.VideoEncoder) {
		return false
	}
	if profile.AudioEncoder != "" && !containsEncoder(capabilities.AudioEncoders, profile.AudioEncoder) {
		return false
	}
	return profile.VideoEncoder != "" || profile.AudioEncoder != ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsEncoder(values []ffmpeg.CodecCapability, want string) bool {
	for _, value := range values {
		if value.Name == want {
			return true
		}
	}
	return false
}

func (a *App) ListFonts() ([]string, error) {
	return platform.ListFonts()
}

func (a *App) GetCapabilities() (ffmpeg.Capabilities, error) {
	engine, _, err := a.runtimeComponents()
	if err != nil {
		return ffmpeg.Capabilities{}, err
	}
	return engine.Capabilities(a.ctx)
}

func (a *App) GetCurrentVersion() string { return version }

func (a *App) CheckForUpdate() (update.Release, error) {
	if err := a.validRuntime(); err != nil {
		return update.Release{}, err
	}
	a.settingsMu.Lock()
	settings := a.settings
	a.settingsMu.Unlock()
	repository := githubRepository
	if repository == "" {
		repository = strings.TrimSpace(os.Getenv("CONVERTGO_GITHUB_REPOSITORY"))
	}
	if repository == "" {
		return update.Release{}, errors.New("updates are unconfigured: this build has no GitHub repository setting")
	}
	source := update.UpdateSource{Kind: update.SourceGitHub}
	if settings.UpdateSource == "mirror" {
		source = update.UpdateSource{Kind: update.SourceMirror, MirrorURL: settings.UpdateMirrorURL}
	}
	return update.Check(a.ctx, repository, source)
}

func (a *App) DownloadUpdate(requestedVersion string) (string, error) {
	release, err := a.CheckForUpdate()
	if err != nil {
		return "", err
	}
	if release.Version != requestedVersion {
		return "", errors.New("requested update is no longer the latest verified release; check again")
	}
	target := update.CurrentTarget()
	var asset *update.Asset
	for i := range release.Assets {
		if release.Assets[i].OS == target.OS && release.Assets[i].Arch == target.Arch {
			asset = &release.Assets[i]
			break
		}
	}
	if asset == nil {
		return "", fmt.Errorf("latest release has no package for %s/%s", target.OS, target.Arch)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate local update cache: %w", err)
	}
	dir := filepath.Join(cache, "ConvertGo", "updates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create local update cache: %w", err)
	}
	filename := filepath.Base(asset.Filename)
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		return "", errors.New("verified update filename is invalid")
	}
	destination := filepath.Join(dir, release.Version+"-"+filename)
	if err := update.Download(a.ctx, release, target, destination); err != nil {
		return "", err
	}
	if err := openDownloadedFile(destination); err != nil {
		return destination, fmt.Errorf("verified update downloaded to %q but could not be opened: %w", destination, err)
	}
	return destination, nil
}

func openDownloadedFile(path string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", path)
	case "windows":
		command = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path)
	case "linux":
		command = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("opening update packages is unsupported on %s", runtime.GOOS)
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func (a *App) onJobChanged(job model.Job) {
	wailsruntime.EventsEmit(a.ctx, "job:changed", job)
	if job.State == model.JobRunning {
		a.acquireSleepInhibitor(job.ID)
		return
	}
	a.finishSleepJob(job.ID)
}

func (a *App) onJobProgress(jobID string, progress model.Progress) {
	wailsruntime.EventsEmit(a.ctx, "job:progress", map[string]any{"jobID": jobID, "progress": progress})
}

type SleepInhibitionStatus struct {
	Active bool   `json:"active"`
	Error  string `json:"error"`
}

func (a *App) GetSleepInhibitionStatus() SleepInhibitionStatus {
	a.sleepMu.Lock()
	defer a.sleepMu.Unlock()
	return SleepInhibitionStatus{Active: a.sleepRelease != nil, Error: a.sleepError}
}

func (a *App) acquireSleepInhibitor(jobID string) {
	a.sleepMu.Lock()
	a.sleepJobs[jobID] = true
	a.sleepMu.Unlock()

	a.settingsMu.Lock()
	prevent := a.settingsErr == nil && a.settings.PreventSleep
	a.settingsMu.Unlock()
	if !prevent {
		return
	}

	a.sleepMu.Lock()
	if a.sleepRelease != nil {
		a.sleepMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	release, err := platform.InhibitSleep(ctx)
	if err != nil {
		cancel()
		a.sleepError = err.Error()
		a.sleepMu.Unlock()
		wailsruntime.EventsEmit(a.ctx, sleepStatusEvent, map[string]any{"active": false, "error": err.Error()})
		return
	}
	a.sleepError = ""
	a.sleepCancel = cancel
	a.sleepRelease = release
	a.sleepMu.Unlock()
	wailsruntime.EventsEmit(a.ctx, sleepStatusEvent, map[string]any{"active": true, "error": ""})
}

func (a *App) finishSleepJob(jobID string) {
	a.sleepMu.Lock()
	delete(a.sleepJobs, jobID)
	if len(a.sleepJobs) != 0 {
		a.sleepMu.Unlock()
		return
	}
	release, cancel := a.sleepRelease, a.sleepCancel
	a.sleepRelease, a.sleepCancel, a.sleepError = nil, nil, ""
	a.sleepMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if release != nil {
		release()
	}
	wailsruntime.EventsEmit(a.ctx, sleepStatusEvent, map[string]any{"active": false, "error": ""})
}

func (a *App) updateSleepPreference(enabled bool) {
	if enabled {
		for _, job := range a.GetJobs() {
			if job.State == model.JobRunning {
				a.acquireSleepInhibitor(job.ID)
			}
		}
		return
	}
	a.sleepMu.Lock()
	for id := range a.sleepJobs {
		delete(a.sleepJobs, id)
	}
	release, cancel := a.sleepRelease, a.sleepCancel
	a.sleepRelease, a.sleepCancel, a.sleepError = nil, nil, ""
	a.sleepMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if release != nil {
		release()
	}
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, sleepStatusEvent, map[string]any{"active": false, "error": ""})
	}
}

func (a *App) releaseSleepInhibitor() {
	a.sleepMu.Lock()
	for id := range a.sleepJobs {
		delete(a.sleepJobs, id)
	}
	release, cancel := a.sleepRelease, a.sleepCancel
	a.sleepRelease, a.sleepCancel, a.sleepError = nil, nil, ""
	a.sleepMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if release != nil {
		release()
	}
}
