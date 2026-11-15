package httpapi

import (
	"audio-processing-service/internal/jobs"
	"audio-processing-service/internal/wav"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func mixPayload(t *testing.T) []byte {
	t.Helper()
	first, err := wav.Encode(wav.Stream{Rate: 8000, Channels: 2, Samples: []int16{30000, -30000, 10000, -10000}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := wav.Encode(wav.Stream{Rate: 8000, Channels: 2, Samples: []int16{30000, -30000}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"tracks": []any{
		map[string]any{"audio": first},
		map[string]any{"audio": second, "start": 1, "gain": 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestMixSyncAndJobProduceSamePersistentOutput(t *testing.T) {
	payload := mixPayload(t)
	syncResponse := httptest.NewRecorder()
	Handler().ServeHTTP(syncResponse, httptest.NewRequest(http.MethodPost, "/v1/mix", bytes.NewReader(payload)))
	if syncResponse.Code != http.StatusOK {
		t.Fatalf("sync status %d: %s", syncResponse.Code, syncResponse.Body.String())
	}
	stream, err := wav.Decode(syncResponse.Body.Bytes())
	if err != nil || !reflect.DeepEqual(stream.Samples, []int16{30000, -30000, 32767, -32768}) {
		t.Fatalf("sync output %+v, error %v", stream, err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manager, err := jobs.Open(root, executable, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterJobs(mux, manager)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/jobs?operation=mix-wav", bytes.NewReader(payload)))
	if response.Code != http.StatusAccepted {
		manager.Close()
		t.Fatalf("job status %d: %s", response.Code, response.Body.String())
	}
	var job jobs.Job
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		manager.Close()
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		job, err = manager.Get(job.ID)
		if err != nil || job.State == "failed" || time.Now().After(deadline) {
			manager.Close()
			t.Fatalf("job %+v, error %v", job, err)
		}
		if job.State == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	manager.Close()
	manager, err = jobs.Open(root, executable, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	output, mime, err := manager.Output(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	result, err := io.ReadAll(output)
	if err != nil || mime != "audio/wav" || !bytes.Equal(result, syncResponse.Body.Bytes()) {
		t.Fatalf("persistent output mismatch: mime=%s error=%v", mime, err)
	}
}

func TestMixRejectsMalformedAndMismatchedInput(t *testing.T) {
	for _, body := range []string{`{}`, `{"tracks":[]} {}`, `{"tracks":[],"unknown":1}`} {
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/mix", bytes.NewBufferString(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status %d", body, response.Code)
		}
	}
	mono, err := wav.Encode(wav.Stream{Rate: 8000, Channels: 1, Samples: []int16{1}})
	if err != nil {
		t.Fatal(err)
	}
	stereo, err := wav.Encode(wav.Stream{Rate: 8000, Channels: 2, Samples: []int16{1, 1}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"tracks": []any{map[string]any{"audio": mono}, map[string]any{"audio": stereo}}})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/mix", bytes.NewReader(body)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("mismatched channels: status %d", response.Code)
	}
}

func TestInvalidMixJobCanRetryWithoutPublishingOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := jobs.Open(t.TempDir(), executable, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	mux := http.NewServeMux()
	RegisterJobs(mux, manager)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/jobs?operation=mix-wav", bytes.NewBufferString(`{"tracks":[]}`)))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	var job jobs.Job
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	waitFailed := func(attempt int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			current, err := manager.Get(job.ID)
			if err != nil || time.Now().After(deadline) {
				t.Fatalf("job %+v, error %v", current, err)
			}
			if current.State == "failed" && current.Attempt == attempt {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitFailed(1)
	if _, _, err := manager.Output(job.ID); err != jobs.ErrConflict {
		t.Fatalf("failed job exposed output: %v", err)
	}
	if _, err := manager.Retry(job.ID); err != nil {
		t.Fatal(err)
	}
	waitFailed(2)
	if _, _, err := manager.Output(job.ID); err != jobs.ErrConflict {
		t.Fatalf("retried failed job exposed output: %v", err)
	}
}

func TestQueuedMixCancellationAndRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell to hold the media worker")
	}
	root := t.TempDir()
	blocker := filepath.Join(root, "slow-ffmpeg")
	if err := os.WriteFile(blocker, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := jobs.Open(filepath.Join(root, "data"), blocker, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	mux := http.NewServeMux()
	RegisterJobs(mux, manager)
	submit := func(operation string, body []byte) jobs.Job {
		t.Helper()
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/jobs?operation="+operation, bytes.NewReader(body)))
		if response.Code != http.StatusAccepted {
			t.Fatalf("submit %s: %d %s", operation, response.Code, response.Body.String())
		}
		var job jobs.Job
		if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		return job
	}
	wait := func(id, state string) jobs.Job {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			job, err := manager.Get(id)
			if err != nil || time.Now().After(deadline) {
				t.Fatalf("waiting for %s: %+v, %v", state, job, err)
			}
			if job.State == state {
				return job
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	busy := submit("audio-m4a", []byte("media"))
	wait(busy.ID, "running")
	queued := submit("mix-wav", mixPayload(t))
	if err := manager.Cancel(queued.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Output(queued.ID); err != jobs.ErrConflict {
		t.Fatalf("cancelled mix exposed output: %v", err)
	}
	if _, err := manager.Retry(queued.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Cancel(busy.ID); err != nil {
		t.Fatal(err)
	}
	completed := wait(queued.ID, "completed")
	if completed.Attempt != 2 {
		t.Fatalf("attempt = %d, want 2", completed.Attempt)
	}
	output, mime, err := manager.Output(queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	data, err := io.ReadAll(output)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := wav.Decode(data)
	if err != nil || mime != "audio/wav" || !reflect.DeepEqual(stream.Samples, []int16{30000, -30000, 32767, -32768}) {
		t.Fatalf("retry output %+v, mime %s, error %v", stream, mime, err)
	}
}
