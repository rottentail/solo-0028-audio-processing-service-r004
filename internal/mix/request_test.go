package mix

import (
	"audio-processing-service/internal/wav"
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func largePayload(t *testing.T, tracks []wav.Stream, gains ...float64) []byte {
	t.Helper()
	entries := make([]map[string]any, 0, len(tracks))
	for i, stream := range tracks {
		encoded, err := wav.Encode(stream)
		if err != nil {
			t.Fatal(err)
		}
		entry := map[string]any{"audio": encoded}
		if i < len(gains) {
			entry["gain"] = gains[i]
		}
		entries = append(entries, entry)
	}
	body, err := json.Marshal(map[string]any{"tracks": entries})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func constantStream(rate, value int16, count int) wav.Stream {
	samples := make([]int16, count)
	for i := range samples {
		samples[i] = value
	}
	return wav.Stream{Rate: uint32(rate), Channels: 1, Samples: samples}
}

func decodeSamples(t *testing.T, encoded []byte) []int16 {
	t.Helper()
	stream, err := wav.Decode(encoded)
	if err != nil {
		t.Fatalf("rendered WAV does not decode: %v", err)
	}
	return stream.Samples
}

// Two same-sized tracks in one request must keep their own audio: the second
// track's decode must not replace the first track's samples.
func TestRenderKeepsSameSizedTracksDistinct(t *testing.T) {
	count := 40000 // large enough to hit any size-classed buffer reuse
	body := largePayload(t, []wav.Stream{constantStream(8000, 1000, count), constantStream(8000, 2000, count)})
	output, err := Render(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	samples := decodeSamples(t, output)
	if len(samples) != count {
		t.Fatalf("got %d samples, want %d", len(samples), count)
	}
	for i, sample := range samples {
		if sample != 3000 {
			t.Fatalf("sample %d = %d, want 1000+2000=3000", i, sample)
		}
	}
}

// Bytes returned by an earlier Render must not change when a later Render of
// the same size class runs.
func TestRenderResultsAreCallerOwned(t *testing.T) {
	count := 40000
	first, err := Render(context.Background(), largePayload(t, []wav.Stream{constantStream(8000, 1000, count)}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(context.Background(), largePayload(t, []wav.Stream{constantStream(8000, 2000, count)}))
	if err != nil {
		t.Fatal(err)
	}
	for i, sample := range decodeSamples(t, first) {
		if sample != 1000 {
			t.Fatalf("first render changed after second render: sample %d = %d, want 1000", i, sample)
		}
	}
	for i, sample := range decodeSamples(t, second) {
		if sample != 2000 {
			t.Fatalf("second render: sample %d = %d, want 2000", i, sample)
		}
	}
}

// Concurrent renders of different mixes must each return their own audio.
func TestRenderConcurrentIsolation(t *testing.T) {
	count := 40000
	payloads := [][]byte{
		largePayload(t, []wav.Stream{constantStream(8000, 1000, count)}),
		largePayload(t, []wav.Stream{constantStream(8000, 2000, count)}),
		largePayload(t, []wav.Stream{constantStream(8000, 3000, count)}),
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			want := int16((worker%3 + 1) * 1000)
			payload := payloads[worker%3]
			for round := 0; round < 25; round++ {
				output, err := Render(context.Background(), payload)
				if err != nil {
					errs <- err
					return
				}
				stream, err := wav.Decode(output)
				if err != nil {
					errs <- err
					return
				}
				for i, sample := range stream.Samples {
					if sample != want {
						t.Errorf("worker %d round %d: sample %d = %d, want %d", worker, round, i, sample, want)
						return
					}
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
