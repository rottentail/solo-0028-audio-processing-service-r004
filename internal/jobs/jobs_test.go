package jobs_test

import (
	"audio-processing-service/internal/jobs"
	"audio-processing-service/internal/mix"
	"audio-processing-service/internal/profile"
	"audio-processing-service/internal/wav"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mixPayload(t *testing.T, value int16, count int) []byte {
	t.Helper()
	samples := make([]int16, count)
	for i := range samples {
		samples[i] = value
	}
	encoded, err := wav.Encode(wav.Stream{Rate: 8000, Channels: 1, Samples: samples})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"tracks": []any{map[string]any{"audio": encoded}}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func waitState(t *testing.T, m *jobs.Manager, id, state string) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		job, err := m.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == state {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s did not reach %s: %+v", id, state, job)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func submitMix(t *testing.T, m *jobs.Manager, payload []byte) jobs.Job {
	t.Helper()
	job, err := m.Create("mix-wav", profile.Settings{}, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	return waitState(t, m, job.ID, "completed")
}

func outputBytes(t *testing.T, m *jobs.Manager, id string) []byte {
	t.Helper()
	f, _, err := m.Output(id)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(f); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func rewriteRecord(t *testing.T, root, id string, mutate func(*jobs.Job)) {
	t.Helper()
	path := filepath.Join(root, id, "job.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var job jobs.Job
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	mutate(&job)
	raw, err = json.Marshal(&job)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// A completed mix job whose stored output does not render from its retained
// input (a historical crossed delivery) must become failed on restart instead
// of serving the wrong audio, must be retryable under the same ID, and must
// not take correctly completed jobs down with it.
func TestRestartRevalidatesCompletedMixAgainstInput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manager, err := jobs.Open(root, executable, 2, 8)
	if err != nil {
		t.Fatal(err)
	}
	const count = 40000
	payloadA := mixPayload(t, 1000, count)
	payloadB := mixPayload(t, 2500, count)
	payloadC := mixPayload(t, 7000, count)
	good := submitMix(t, manager, payloadB)
	crossed := submitMix(t, manager, payloadA)
	stripped := submitMix(t, manager, payloadA)
	wantA := outputBytes(t, manager, crossed.ID)
	wantB := outputBytes(t, manager, good.ID)
	manager.Close()

	// Cross one job on disk: someone else's mix lands in its output while the
	// recorded hashes are updated to match, exactly as the in-flight
	// corruption persisted them.
	foreign, err := mix.Render(context.Background(), payloadC)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(foreign, wantA) {
		t.Fatal("test payloads must differ")
	}
	if err := os.WriteFile(filepath.Join(root, crossed.ID, "output"), foreign, 0600); err != nil {
		t.Fatal(err)
	}
	rewriteRecord(t, root, crossed.ID, func(j *jobs.Job) {
		j.PCMBuild.OutputSHA256 = sha256Hex(foreign)
		j.PCMBuild.OutputBytes = int64(len(foreign))
	})
	// A second crossed job without any provenance record.
	if err := os.WriteFile(filepath.Join(root, stripped.ID, "output"), foreign, 0600); err != nil {
		t.Fatal(err)
	}
	rewriteRecord(t, root, stripped.ID, func(j *jobs.Job) { j.PCMBuild = nil })

	manager, err = jobs.Open(root, executable, 2, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	// The untouched job stays completed and serves its original bytes.
	if job, err := manager.Get(good.ID); err != nil || job.State != "completed" {
		t.Fatalf("good job after restart: %+v, %v", job, err)
	}
	if got := outputBytes(t, manager, good.ID); !bytes.Equal(got, wantB) {
		t.Fatal("good job output changed across restart")
	}

	// Both crossed jobs are failed and no longer downloadable.
	for _, id := range []string{crossed.ID, stripped.ID} {
		job, err := manager.Get(id)
		if err != nil || job.State != "failed" {
			t.Fatalf("crossed job after restart: %+v, %v", job, err)
		}
		if _, _, err := manager.Output(id); err != jobs.ErrConflict {
			t.Fatalf("crossed job still exposes output: %v", err)
		}
	}

	// Each crossed job resumes under its original ID and publishes its own mix.
	for _, id := range []string{crossed.ID, stripped.ID} {
		if _, err := manager.Retry(id); err != nil {
			t.Fatal(err)
		}
		waitState(t, manager, id, "completed")
		if got := outputBytes(t, manager, id); !bytes.Equal(got, wantA) {
			t.Fatalf("retried job %s did not republish its own mix", id)
		}
	}
}

// Concurrently processed mix jobs must each publish their own submitted audio.
func TestConcurrentMixJobsPublishOwnOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := jobs.Open(t.TempDir(), executable, 2, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	const count = 40000
	type submission struct {
		id   string
		want []byte
	}
	var submissions []submission
	for value := int16(500); value <= 4000; value += 500 {
		payload := mixPayload(t, value, count)
		job, err := manager.Create("mix-wav", profile.Settings{}, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		want, err := mix.Render(context.Background(), payload)
		if err != nil {
			t.Fatal(err)
		}
		submissions = append(submissions, submission{id: job.ID, want: want})
	}
	for _, s := range submissions {
		waitState(t, manager, s.id, "completed")
	}
	for _, s := range submissions {
		if got := outputBytes(t, manager, s.id); !bytes.Equal(got, s.want) {
			t.Fatalf("job %s published another submission's mix", s.id)
		}
	}
}
