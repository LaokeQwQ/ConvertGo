package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"ConvertGo/internal/config"
	"ConvertGo/internal/ffmpeg"
	"ConvertGo/internal/model"
)

func TestAddRequiresOverwriteConfirmation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX scripts")
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "existing.mp4")
	if err := os.WriteFile(output, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	probe := executable(t, "#!/bin/sh\nprintf '{\"format\":{\"format_name\":\"mp4\",\"duration\":\"1\"},\"streams\":[{\"index\":0,\"codec_type\":\"video\",\"codec_name\":\"h264\",\"width\":320,\"height\":180}]}\n'\n")
	manager, err := NewManager(ffmpeg.NewEngine("/not-used", probe), 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := jobSpec(filepath.Join(dir, "input.mp4"), output)
	if err := os.WriteFile(spec.Inputs[0], []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = manager.Add(spec)
	if err == nil || !strings.HasPrefix(err.Error(), "OUTPUT_EXISTS:") {
		t.Fatalf("Add conflict error = %v, want OUTPUT_EXISTS prefix", err)
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "old" {
		t.Fatalf("existing output changed after conflict: %q, %v", data, err)
	}
}

func TestQueueTransitionsAndAtomicOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX scripts")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "done.mp4")
	if err := os.WriteFile(input, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	probe := probeHelper(t)
	engine := ffmpeg.NewEngine(successHelper(t), probe)
	var mu sync.Mutex
	var states []model.JobState
	manager, err := NewManager(engine, 1, func(job model.Job) {
		mu.Lock()
		states = append(states, job.State)
		mu.Unlock()
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.Add(jobSpec(input, output))
	if err != nil {
		t.Fatal(err)
	}
	if job.State != model.JobQueued {
		t.Fatalf("initial state = %q", job.State)
	}
	if err := manager.StartQueue(); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown()
	waitForState(t, manager, job.ID, model.JobCompleted)
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "encoded" {
		t.Fatalf("output = %q, %v", data, err)
	}
	mu.Lock()
	defer mu.Unlock()
	seen := map[model.JobState]bool{}
	for _, state := range states {
		seen[state] = true
	}
	for _, state := range []model.JobState{model.JobQueued, model.JobProbing, model.JobRunning, model.JobCompleted} {
		if !seen[state] {
			t.Errorf("state %q was not emitted; observed %v", state, states)
		}
	}
}

func TestCancelRemovesPartialOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX scripts")
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.mp4"), filepath.Join(dir, "cancelled.mp4")
	if err := os.WriteFile(input, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(ffmpeg.NewEngine(partialSleepHelper(t), probeHelper(t)), 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.Add(jobSpec(input, output))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartQueue(); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown()
	waitForState(t, manager, job.ID, model.JobRunning)
	if err := manager.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager, job.ID, model.JobCancelled)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("cancelled target exists: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Errorf("partial temp output remains: %s", entry.Name())
		}
	}
}

func TestQueueCapsConcurrentProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX scripts")
	}
	dir := t.TempDir()
	probe := probeHelper(t)
	counter := filepath.Join(dir, "active")
	maximum := filepath.Join(dir, "maximum")
	lock := filepath.Join(dir, "lock")
	engine := ffmpeg.NewEngine(concurrencyHelper(t, counter, maximum, lock, "0.1"), probe)
	manager, err := NewManager(engine, 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		input := filepath.Join(dir, fmt.Sprintf("input-%d.mp4", i))
		if err := os.WriteFile(input, []byte("source"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Add(jobSpec(input, filepath.Join(dir, fmt.Sprintf("output-%d.mp4", i)))); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.StartQueue(); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown()
	waitForAll(t, manager, 3, model.JobCompleted)
	data, err := os.ReadFile(maximum)
	if err != nil || strings.TrimSpace(string(data)) != "1" {
		t.Fatalf("maximum concurrent FFmpeg processes = %q, %v, want 1", data, err)
	}
}

func TestSetMaxConcurrentWhileQueued(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX scripts")
	}
	if runtime.NumCPU() < 2 {
		t.Skip("requires at least two logical CPUs")
	}
	dir := t.TempDir()
	counter, maximum, lock := filepath.Join(dir, "active"), filepath.Join(dir, "maximum"), filepath.Join(dir, "lock")
	manager, err := NewManager(ffmpeg.NewEngine(concurrencyHelper(t, counter, maximum, lock, "0.2"), probeHelper(t)), 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		input := filepath.Join(dir, fmt.Sprintf("input-%d.mp4", i))
		if err := os.WriteFile(input, []byte("source"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Add(jobSpec(input, filepath.Join(dir, fmt.Sprintf("output-%d.mp4", i)))); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.SetMaxConcurrent(2); err != nil {
		t.Fatal(err)
	}
	if err := manager.StartQueue(); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown()
	waitForAll(t, manager, 3, model.JobCompleted)
	data, err := os.ReadFile(maximum)
	if err != nil || strings.TrimSpace(string(data)) != "2" {
		t.Fatalf("max concurrency after increase = %q, %v, want 2", data, err)
	}
}

func TestSetMaxConcurrentWhileActiveDefersQueuedJobs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX scripts")
	}
	if runtime.NumCPU() < 2 {
		t.Skip("requires at least two logical CPUs")
	}
	dir := t.TempDir()
	counter, maximum, lock := filepath.Join(dir, "active"), filepath.Join(dir, "maximum"), filepath.Join(dir, "lock")
	manager, err := NewManager(ffmpeg.NewEngine(concurrencyHelper(t, counter, maximum, lock, "0.7"), probeHelper(t)), 2, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		input := filepath.Join(dir, fmt.Sprintf("input-%d.mp4", i))
		if err := os.WriteFile(input, []byte("source"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Add(jobSpec(input, filepath.Join(dir, fmt.Sprintf("output-%d.mp4", i)))); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.StartQueue(); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown()
	waitForRunningCount(t, manager, 2)
	if err := manager.SetMaxConcurrent(1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	queued, running := countStates(manager, model.JobQueued), countStates(manager, model.JobRunning)
	if queued != 1 || running != 2 {
		t.Fatalf("after lowering cap with two active jobs: queued=%d running=%d, jobs=%+v", queued, running, manager.GetJobs())
	}
	waitForAll(t, manager, 3, model.JobCompleted)
}

func TestSetEngineRetainsQueuedJobsAndRejectsActiveTransition(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses POSIX scripts")
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.mp4"), filepath.Join(dir, "output.mp4")
	if err := os.WriteFile(input, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(ffmpeg.NewEngine(partialSleepHelper(t), probeHelper(t)), 1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.Add(jobSpec(input, output))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetEngine(ffmpeg.NewEngine("replacement", "replacement-probe")); err != nil {
		t.Fatal(err)
	}
	if jobs := manager.GetJobs(); len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("queued jobs not retained after engine change: %+v", jobs)
	}
	if err := manager.SetEngine(ffmpeg.NewEngine("second", "second-probe")); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetEngine(nil); err == nil {
		t.Fatal("nil engine accepted")
	}
	if err := manager.SetEngine(ffmpeg.NewEngine(partialSleepHelper(t), probeHelper(t))); err != nil {
		t.Fatal(err)
	}
	if err := manager.StartQueue(); err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager, job.ID, model.JobRunning)
	if err := manager.SetEngine(ffmpeg.NewEngine("new", "new-probe")); err == nil {
		t.Fatal("engine changed while a process was running")
	}
	manager.Shutdown()
	waitForState(t, manager, job.ID, model.JobCancelled)
}

func jobSpec(input, output string) model.JobSpec {
	return model.JobSpec{Mode: model.JobModeIndividual, Inputs: []string{input}, OutputPath: output, Profile: config.Profile{ID: "p", Name: "P", Container: "mp4", VideoEncoder: "libx264"}}
}

func probeHelper(t *testing.T) string {
	t.Helper()
	return executable(t, "#!/bin/sh\nprintf '{\"format\":{\"format_name\":\"mp4\",\"duration\":\"1\"},\"streams\":[{\"index\":0,\"codec_type\":\"video\",\"codec_name\":\"h264\",\"width\":320,\"height\":180}]}'\n")
}

func successHelper(t *testing.T) string {
	t.Helper()
	return executable(t, "#!/bin/sh\nfor arg do output=$arg; done\nprintf encoded > \"$output\"\nprintf 'frame=1\\nout_time=00:00:01.000000\\nprogress=end\\n'\n")
}

func partialSleepHelper(t *testing.T) string {
	t.Helper()
	return executable(t, "#!/bin/sh\nfor arg do output=$arg; done\nprintf partial > \"$output\"\nexec sleep 10\n")
}

func concurrencyHelper(t *testing.T, counter, maximum, lock, delay string) string {
	t.Helper()
	script := "#!/bin/sh\nwhile ! mkdir '" + lock + "' 2>/dev/null; do sleep 0.01; done\nactive=$(cat '" + counter + "' 2>/dev/null || echo 0)\nactive=$((active+1))\necho $active > '" + counter + "'\nmax=$(cat '" + maximum + "' 2>/dev/null || echo 0)\nif [ $active -gt $max ]; then echo $active > '" + maximum + "'; fi\nrmdir '" + lock + "'\nsleep " + delay + "\nwhile ! mkdir '" + lock + "' 2>/dev/null; do sleep 0.01; done\nactive=$(cat '" + counter + "')\necho $((active-1)) > '" + counter + "'\nrmdir '" + lock + "'\nfor arg do output=$arg; done\nprintf encoded > \"$output\"\n"
	return executable(t, script)
}

func executable(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForState(t *testing.T, manager *Manager, id string, state model.JobState) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, job := range manager.GetJobs() {
			if job.ID == id && job.State == state {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("job %s did not reach %q: %+v", id, state, manager.GetJobs())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func countStates(manager *Manager, state model.JobState) int {
	count := 0
	for _, job := range manager.GetJobs() {
		if job.State == state {
			count++
		}
	}
	return count
}

func waitForRunningCount(t *testing.T, manager *Manager, count int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for countStates(manager, model.JobRunning) < count {
		select {
		case <-deadline:
			t.Fatalf("only %d jobs reached running: %+v", countStates(manager, model.JobRunning), manager.GetJobs())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func waitForAll(t *testing.T, manager *Manager, count int, state model.JobState) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		jobs := manager.GetJobs()
		if len(jobs) == count {
			all := true
			for _, job := range jobs {
				all = all && job.State == state
			}
			if all {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("jobs did not reach %q: %+v", state, jobs)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
