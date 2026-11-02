package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"ConvertGo/internal/ffmpeg"
	"ConvertGo/internal/media"
	"ConvertGo/internal/model"
)

type OutputExistsError struct{ Path string }

func (e *OutputExistsError) Error() string { return "OUTPUT_EXISTS: " + e.Path }

type Manager struct {
	engine        *ffmpeg.Engine
	maxConcurrent int
	onJob         func(model.Job)
	onProgress    func(string, model.Progress)

	mu              sync.Mutex
	jobs            map[string]model.Job
	order           []string
	outputs         map[string]string
	cancel          map[string]context.CancelFunc
	contexts        map[string]context.Context
	activeJobs      int
	activeProcesses int
	lifeCtx         context.Context
	lifeCancel      context.CancelFunc
	shutdown        bool
	wake            chan struct{}
	processChanged  chan struct{}
	started         bool
	workers         sync.WaitGroup
}

func NewManager(engine *ffmpeg.Engine, maxConcurrent int, onJob func(model.Job), onProgress func(string, model.Progress)) (*Manager, error) {
	if engine == nil {
		return nil, errors.New("ffmpeg engine is required")
	}
	if maxConcurrent < 1 || maxConcurrent > runtime.NumCPU() {
		return nil, fmt.Errorf("max concurrent jobs must be between 1 and %d", runtime.NumCPU())
	}
	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	return &Manager{
		engine: engine, maxConcurrent: maxConcurrent, onJob: onJob, onProgress: onProgress,
		jobs: make(map[string]model.Job), outputs: make(map[string]string), cancel: make(map[string]context.CancelFunc), contexts: make(map[string]context.Context),
		wake: make(chan struct{}, runtime.NumCPU()), processChanged: make(chan struct{}), lifeCtx: lifeCtx, lifeCancel: lifeCancel,
	}, nil
}

func (m *Manager) SetMaxConcurrent(max int) error {
	if max < 1 || max > runtime.NumCPU() {
		return fmt.Errorf("max concurrent jobs must be between 1 and %d", runtime.NumCPU())
	}
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return errors.New("job manager is shut down")
	}
	m.maxConcurrent = max
	m.wakeProcessWaitersLocked()
	started := m.started
	m.mu.Unlock()
	if started {
		for range runtime.NumCPU() {
			m.signal()
		}
	}
	return nil
}

func (m *Manager) SetEngine(engine *ffmpeg.Engine) error {
	if engine == nil {
		return errors.New("ffmpeg engine is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shutdown {
		return errors.New("job manager is shut down")
	}
	for _, job := range m.jobs {
		if job.State == model.JobProbing || job.State == model.JobRunning {
			return errors.New("cannot change ffmpeg engine while a job is active")
		}
	}
	m.engine = engine
	return nil
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	alreadyShutdown := m.shutdown
	var cancels []context.CancelFunc
	var changed []model.Job
	if !alreadyShutdown {
		m.shutdown = true
		m.lifeCancel()
		m.wakeProcessWaitersLocked()
		for id, job := range m.jobs {
			if job.State == model.JobQueued {
				job.State = model.JobCancelled
				m.jobs[id] = job
				changed = append(changed, job)
			}
			if cancel := m.cancel[id]; cancel != nil {
				cancels = append(cancels, cancel)
			}
		}
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	for _, job := range changed {
		m.notifyJob(job)
	}
	m.workers.Wait()
	for {
		m.mu.Lock()
		if m.activeProcesses == 0 {
			m.mu.Unlock()
			return
		}
		changed := m.processChanged
		m.mu.Unlock()
		<-changed
	}
}

func (m *Manager) Add(spec model.JobSpec) (model.Job, error) {
	spec.ApplyDefaults()
	if err := spec.Validate(); err != nil {
		return model.Job{}, err
	}
	if spec.Mode == model.JobModeConcatenate && spec.SubtitleMode != model.SubtitleModeNone {
		return model.Job{}, errors.New("subtitle copy and burn-in are unavailable in concatenate mode")
	}
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return model.Job{}, errors.New("job manager is shut down")
	}
	engine, lifeCtx := m.engine, m.lifeCtx
	m.mu.Unlock()
	target, err := filepath.Abs(spec.OutputPath)
	if err != nil {
		return model.Job{}, fmt.Errorf("resolve output path: %w", err)
	}
	spec.OutputPath = target
	targetKey := filepath.Clean(target)
	if runtime.GOOS == "windows" {
		targetKey = strings.ToLower(targetKey)
	}
	if err := validateTarget(target); err != nil {
		return model.Job{}, err
	}
	if _, err := os.Lstat(target); err == nil && !spec.OverwriteConfirmed {
		return model.Job{}, &OutputExistsError{Path: target}
	} else if err != nil && !os.IsNotExist(err) {
		return model.Job{}, fmt.Errorf("inspect output path: %w", err)
	}
	m.mu.Lock()
	if owner, exists := m.outputs[targetKey]; exists {
		m.mu.Unlock()
		return model.Job{}, fmt.Errorf("output path is already reserved by job %s", owner)
	}
	m.mu.Unlock()

	// Probe synchronously so invalid inputs and incompatible concatenations never
	// enter the queue. The worker probes again immediately before conversion.
	infos := make([]media.MediaInfo, len(spec.Inputs))
	for i, input := range spec.Inputs {
		if err := media.ValidateLocalPath(input); err != nil {
			return model.Job{}, err
		}
		absoluteInput, err := filepath.Abs(input)
		if err != nil {
			return model.Job{}, fmt.Errorf("resolve input path: %w", err)
		}
		spec.Inputs[i] = absoluteInput
		if err := m.acquireProcess(lifeCtx); err != nil {
			return model.Job{}, err
		}
		info, probeErr := media.Probe(lifeCtx, engine.FFprobePath(), absoluteInput)
		m.releaseProcess()
		if probeErr != nil {
			return model.Job{}, fmt.Errorf("probe input %q: %w", input, probeErr)
		}
		infos[i] = info
		if err := validateSpecForInput(spec, info); err != nil {
			return model.Job{}, fmt.Errorf("input %q: %w", input, err)
		}
	}
	if spec.Mode == model.JobModeConcatenate {
		for i := 1; i < len(infos); i++ {
			if !sameLayout(infos[0], infos[i]) {
				return model.Job{}, fmt.Errorf("concatenate input %q has a different stream layout", infos[i].Path)
			}
		}
		if spec.VideoStream == nil {
			for _, stream := range infos[0].Streams {
				if stream.Type == "video" {
					index := stream.Index
					spec.VideoStream = &index
					break
				}
			}
		}
		if len(spec.AudioStreams) == 0 {
			for _, stream := range infos[0].Streams {
				if stream.Type == "audio" {
					spec.AudioStreams = append(spec.AudioStreams, stream.Index)
				}
			}
		}
	}
	id, err := newID()
	if err != nil {
		return model.Job{}, err
	}
	job := model.Job{ID: id, Spec: spec, State: model.JobQueued}
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return model.Job{}, errors.New("job manager is shut down")
	}
	if owner, exists := m.outputs[targetKey]; exists {
		m.mu.Unlock()
		return model.Job{}, fmt.Errorf("output path is already reserved by job %s", owner)
	}
	m.jobs[id] = job
	m.order = append(m.order, id)
	m.outputs[targetKey] = id
	started := m.started
	m.mu.Unlock()
	m.notifyJob(job)
	if started {
		m.signal()
	}
	return job, nil
}

func (m *Manager) StartQueue() error {
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return errors.New("job manager is shut down")
	}
	if m.started {
		m.mu.Unlock()
		return errors.New("queue is already started")
	}
	m.started = true
	m.workers.Add(runtime.NumCPU())
	m.mu.Unlock()
	for range runtime.NumCPU() {
		go func() {
			defer m.workers.Done()
			m.worker()
		}()
	}
	for range runtime.NumCPU() {
		m.signal()
	}
	return nil
}

func (m *Manager) Cancel(jobID string) error {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return errors.New("job not found")
	}
	if job.State == model.JobQueued {
		job.State = model.JobCancelled
		m.jobs[jobID] = job
		delete(m.cancel, jobID)
		m.mu.Unlock()
		m.notifyJob(job)
		return nil
	}
	cancel := m.cancel[jobID]
	if job.State != model.JobProbing && job.State != model.JobRunning || cancel == nil {
		m.mu.Unlock()
		return errors.New("job is not active")
	}
	m.mu.Unlock()
	cancel()
	return nil
}

func (m *Manager) RemoveJob(jobID string) error {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	if !ok {
		m.mu.Unlock()
		return errors.New("job not found")
	}
	if job.State == model.JobProbing || job.State == model.JobRunning {
		m.mu.Unlock()
		return errors.New("active job must be cancelled before removal")
	}
	delete(m.jobs, jobID)
	for i, id := range m.order {
		if id == jobID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	delete(m.cancel, jobID)
	key, _ := outputKey(job.Spec.OutputPath)
	if m.outputs[key] == jobID {
		delete(m.outputs, key)
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) GetJobs() []model.Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := make([]model.Job, 0, len(m.order))
	for _, id := range m.order {
		if job, ok := m.jobs[id]; ok {
			jobs = append(jobs, job)
		}
	}
	return jobs
}

func (m *Manager) worker() {
	for {
		select {
		case <-m.lifeCtx.Done():
			return
		case <-m.wake:
		}
		job, ok := m.nextQueued()
		if !ok {
			continue
		}
		m.process(job)
		m.mu.Lock()
		m.activeJobs--
		m.mu.Unlock()
		m.signal()
	}
}

func (m *Manager) nextQueued() (model.Job, bool) {
	m.mu.Lock()
	if m.shutdown || m.activeJobs >= m.maxConcurrent {
		m.mu.Unlock()
		return model.Job{}, false
	}
	for _, id := range m.order {
		job := m.jobs[id]
		if job.State == model.JobQueued {
			job.State = model.JobProbing
			ctx, cancel := context.WithCancel(m.lifeCtx)
			m.contexts[id] = ctx
			m.cancel[id] = cancel
			m.jobs[id] = job
			m.activeJobs++
			m.mu.Unlock()
			m.notifyJob(job)
			return job, true
		}
	}
	m.mu.Unlock()
	return model.Job{}, false
}

func (m *Manager) process(job model.Job) {
	m.mu.Lock()
	ctx := m.contexts[job.ID]
	m.mu.Unlock()
	if ctx == nil {
		m.finish(job.ID, errors.New("job context is missing"))
		return
	}
	var totalDuration time.Duration
	for _, input := range job.Spec.Inputs {
		if err := m.acquireProcess(ctx); err != nil {
			m.finish(job.ID, err)
			return
		}
		info, err := media.Probe(ctx, m.engine.FFprobePath(), input)
		m.releaseProcess()
		if err != nil {
			m.finish(job.ID, err)
			return
		}
		if err := validateSpecForInput(job.Spec, info); err != nil {
			m.finish(job.ID, err)
			return
		}
		totalDuration += time.Duration(info.DurationSeconds * float64(time.Second))
	}
	temp, err := tempOutput(job.Spec.OutputPath)
	if err != nil {
		m.finish(job.ID, err)
		return
	}
	defer os.Remove(temp)
	spec := job.Spec
	spec.OutputPath = temp
	args, err := ffmpeg.BuildArgs(spec)
	if err != nil {
		m.finish(job.ID, err)
		return
	}
	m.setState(job.ID, model.JobRunning, "")
	duration := totalDuration
	if job.Spec.TrimEnd > 0 {
		duration = (job.Spec.TrimEnd - job.Spec.TrimStart) * time.Duration(len(job.Spec.Inputs))
	} else if job.Spec.TrimStart > 0 {
		duration -= job.Spec.TrimStart * time.Duration(len(job.Spec.Inputs))
	}
	if err := m.acquireProcess(ctx); err != nil {
		m.finish(job.ID, err)
		return
	}
	err = m.engine.Run(ctx, args, func(progress model.Progress) {
		progress.TotalDuration = duration
		m.mu.Lock()
		current, ok := m.jobs[job.ID]
		if ok && current.State == model.JobRunning {
			current.Progress = progress
			m.jobs[job.ID] = current
		}
		m.mu.Unlock()
		if m.onProgress != nil {
			m.onProgress(job.ID, progress)
		}
	})
	m.releaseProcess()
	if err != nil {
		m.finish(job.ID, err)
		return
	}
	if err := m.commitOutput(job.ID, ctx, temp, job.Spec.OutputPath, job.Spec.OverwriteConfirmed); err != nil {
		m.finish(job.ID, err)
	}
}

func (m *Manager) commitOutput(id string, ctx context.Context, temp, target string, overwrite bool) error {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok || job.State != model.JobRunning {
		m.mu.Unlock()
		return errors.New("job is no longer running")
	}
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return err
	}
	if err := replaceOutput(temp, target, overwrite); err != nil {
		m.mu.Unlock()
		return err
	}
	job.State, job.Error = model.JobCompleted, ""
	m.jobs[id] = job
	cancel := m.cancel[id]
	delete(m.cancel, id)
	delete(m.contexts, id)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.notifyJob(job)
	return nil
}

func (m *Manager) setState(id string, state model.JobState, message string) {
	m.mu.Lock()
	job := m.jobs[id]
	job.State, job.Error = state, message
	m.jobs[id] = job
	m.mu.Unlock()
	m.notifyJob(job)
}

func (m *Manager) finish(id string, err error) {
	m.mu.Lock()
	job := m.jobs[id]
	if err == nil {
		job.State, job.Error = model.JobCompleted, ""
	} else if errors.Is(err, context.Canceled) {
		job.State, job.Error = model.JobCancelled, ""
	} else {
		job.State, job.Error = model.JobFailed, err.Error()
	}
	m.jobs[id] = job
	cancel := m.cancel[id]
	delete(m.cancel, id)
	delete(m.contexts, id)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	m.notifyJob(job)
}

func (m *Manager) notifyJob(job model.Job) {
	if m.onJob != nil {
		m.onJob(job)
	}
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) acquireProcess(ctx context.Context) error {
	for {
		m.mu.Lock()
		if m.shutdown || ctx.Err() != nil {
			m.mu.Unlock()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("job manager is shut down")
		}
		if m.activeProcesses < m.maxConcurrent {
			m.activeProcesses++
			m.mu.Unlock()
			return nil
		}
		changed := m.processChanged
		m.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Manager) releaseProcess() {
	m.mu.Lock()
	m.activeProcesses--
	m.wakeProcessWaitersLocked()
	m.mu.Unlock()
}

func (m *Manager) wakeProcessWaitersLocked() {
	close(m.processChanged)
	m.processChanged = make(chan struct{})
}

func validateTarget(target string) error {
	info, err := os.Stat(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("output directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("output parent is not a directory")
	}
	if strings.TrimSpace(filepath.Base(target)) == "." || strings.TrimSpace(filepath.Base(target)) == string(filepath.Separator) {
		return errors.New("output path must name a file")
	}
	return nil
}

func validateSpecForInput(spec model.JobSpec, info media.MediaInfo) error {
	if info.DurationSeconds > 0 && (spec.TrimStart.Seconds() >= info.DurationSeconds || spec.TrimEnd > 0 && spec.TrimEnd.Seconds() > info.DurationSeconds) {
		return errors.New("trim interval exceeds input duration")
	}
	var selectedVideo *media.StreamInfo
	for i := range info.Streams {
		stream := &info.Streams[i]
		if stream.Type == "video" && (spec.VideoStream == nil || stream.Index == *spec.VideoStream) {
			selectedVideo = stream
			break
		}
	}
	if spec.VideoStream != nil && selectedVideo == nil {
		return errors.New("selected video stream does not exist")
	}
	if spec.Crop != nil {
		if selectedVideo == nil {
			return errors.New("crop requires a selected video stream")
		}
		if spec.Crop.X+spec.Crop.Width > selectedVideo.Width || spec.Crop.Y+spec.Crop.Height > selectedVideo.Height {
			return errors.New("crop rectangle exceeds input dimensions")
		}
	}
	for _, index := range spec.AudioStreams {
		found := false
		for _, stream := range info.Streams {
			if stream.Index == index && stream.Type == "audio" {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("selected audio stream %d does not exist", index)
		}
	}
	if spec.SubtitleMode == model.SubtitleModeNone {
		return nil
	}
	var subtitle *media.StreamInfo
	for i := range info.Streams {
		stream := &info.Streams[i]
		if stream.Index == *spec.SubtitleStream && stream.Type == "subtitle" {
			subtitle = stream
			break
		}
	}
	if subtitle == nil {
		return errors.New("selected subtitle stream does not exist")
	}
	codec, container := strings.ToLower(subtitle.Codec), strings.ToLower(spec.Profile.Container)
	if spec.SubtitleMode == model.SubtitleModeBurn {
		switch codec {
		case "ass", "ssa", "subrip", "srt", "webvtt", "mov_text":
			return nil
		default:
			return fmt.Errorf("subtitle codec %q cannot be burned in", subtitle.Codec)
		}
	}
	if !subtitleCopyCompatible(container, codec) {
		return fmt.Errorf("subtitle codec %q cannot be copied into %s", subtitle.Codec, spec.Profile.Container)
	}
	return nil
}

func subtitleCopyCompatible(container, codec string) bool {
	switch container {
	case "mp4", "mov":
		return codec == "mov_text"
	case "webm":
		return codec == "webvtt"
	case "matroska", "mkv":
		switch codec {
		case "ass", "ssa", "subrip", "srt", "webvtt", "mov_text", "dvd_subtitle", "hdmv_pgs_subtitle":
			return true
		}
	}
	return false
}

func sameLayout(a, b media.MediaInfo) bool {
	if len(a.Streams) != len(b.Streams) {
		return false
	}
	for i := range a.Streams {
		x, y := a.Streams[i], b.Streams[i]
		if x.Index != y.Index || x.Type != y.Type || x.Codec != y.Codec || x.Width != y.Width || x.Height != y.Height || x.FrameRate != y.FrameRate || x.SampleRate != y.SampleRate || x.Channels != y.Channels {
			return false
		}
	}
	return true
}

func tempOutput(target string) (string, error) {
	dir, base := filepath.Dir(target), filepath.Base(target)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	file, err := os.CreateTemp(dir, "."+stem+"-*.tmp"+ext)
	if err != nil {
		return "", fmt.Errorf("create temporary output: %w", err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}

func replaceOutput(temp, target string, overwrite bool) error {
	if _, err := os.Lstat(target); err == nil && !overwrite {
		return &OutputExistsError{Path: target}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Rename(temp, target); err != nil {
			return fmt.Errorf("replace output: %w", err)
		}
		return nil
	}
	backup := ""
	if _, err := os.Lstat(target); err == nil {
		backup = target + ".convertgo-backup-" + filepath.Base(temp)
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("preserve existing output: %w", err)
		}
	}
	if err := os.Rename(temp, target); err != nil {
		if backup != "" {
			if restoreErr := os.Rename(backup, target); restoreErr != nil {
				return fmt.Errorf("replace output failed: %w (restore existing output failed: %v)", err, restoreErr)
			}
		}
		return fmt.Errorf("replace output: %w", err)
	}
	if backup != "" {
		if err := os.Remove(backup); err != nil {
			return fmt.Errorf("new output installed but removing backup failed: %w", err)
		}
	}
	return nil
}

func outputKey(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	key := filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key, nil
}

func newID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate job ID: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
