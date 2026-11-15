package audio

import (
	"audio-processing-service/internal/wav"
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestMixAlignsFramesAndSaturatesAfterSumming(t *testing.T) {
	tracks := []MixTrack{
		{Stream: wav.Stream{Rate: 8000, Channels: 2, Samples: []int16{30000, -30000, 10000, -10000}}, Gain: 1},
		{Stream: wav.Stream{Rate: 8000, Channels: 2, Samples: []int16{30000, -30000}}, Start: 1, Gain: 1},
	}
	got, err := Mix(context.Background(), tracks)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int16{30000, -30000, 32767, -32768}; !reflect.DeepEqual(got.Samples, want) {
		t.Fatalf("samples %v, want %v", got.Samples, want)
	}
	if got.Rate != 8000 || got.Channels != 2 || got.Frames() != 2 {
		t.Fatalf("unexpected output format: %+v", got)
	}
}

func TestMixRejectsInvalidTracksAndCancellation(t *testing.T) {
	valid := MixTrack{Stream: wav.Stream{Rate: 8000, Channels: 1, Samples: []int16{1}}, Gain: 1}
	for _, tracks := range [][]MixTrack{
		nil,
		{valid, {Stream: wav.Stream{Rate: 16000, Channels: 1, Samples: []int16{1}}, Gain: 1}},
		{{Stream: valid.Stream, Start: -1, Gain: 1}},
		{{Stream: valid.Stream, Start: MaxMixBytes, Gain: 1}},
		{{Stream: valid.Stream, Gain: 5}},
	} {
		if _, err := Mix(context.Background(), tracks); err == nil {
			t.Fatalf("accepted invalid tracks: %+v", tracks)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Mix(ctx, []MixTrack{valid}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
}
