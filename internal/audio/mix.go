package audio

import (
	"audio-processing-service/internal/wav"
	"context"
	"errors"
	"math"
)

// MaxMixBytes includes the standard WAV header.
const MaxMixBytes = 16 << 20

type MixTrack struct {
	Stream wav.Stream
	Start  int
	Gain   float64
}

// Mix aligns tracks on frame boundaries and saturates only after summing.
func Mix(ctx context.Context, tracks []MixTrack) (wav.Stream, error) {
	if len(tracks) == 0 || len(tracks) > 16 {
		return wav.Stream{}, errors.New("mix requires between 1 and 16 tracks")
	}
	first := tracks[0].Stream
	if first.Rate == 0 || (first.Channels != 1 && first.Channels != 2) {
		return wav.Stream{}, errors.New("invalid track format")
	}
	channels := int(first.Channels)
	maxFrames := (MaxMixBytes - 44) / (channels * 2)
	frames := 0
	for _, track := range tracks {
		s := track.Stream
		if s.Rate != first.Rate || s.Channels != first.Channels || len(s.Samples)%channels != 0 || len(s.Samples) == 0 {
			return wav.Stream{}, errors.New("tracks must be nonempty and have the same rate and channels")
		}
		if math.IsNaN(track.Gain) || math.IsInf(track.Gain, 0) || track.Gain < 0 || track.Gain > 4 {
			return wav.Stream{}, errors.New("gain must be between 0 and 4")
		}
		trackFrames := len(s.Samples) / channels
		if track.Start < 0 || track.Start > maxFrames || trackFrames > maxFrames-track.Start {
			return wav.Stream{}, errors.New("mix exceeds output limit")
		}
		if end := track.Start + trackFrames; end > frames {
			frames = end
		}
	}
	if err := ctx.Err(); err != nil {
		return wav.Stream{}, err
	}
	sums := make([]float64, frames*channels)
	for _, track := range tracks {
		offset := track.Start * channels
		for i, sample := range track.Stream.Samples {
			if i%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return wav.Stream{}, err
				}
			}
			sums[offset+i] += float64(sample) * track.Gain
		}
	}
	result := wav.Stream{Rate: first.Rate, Channels: first.Channels, Samples: make([]int16, len(sums))}
	for i, sum := range sums {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return wav.Stream{}, err
			}
		}
		value := math.Round(sum)
		if value > 32767 {
			value = 32767
		} else if value < -32768 {
			value = -32768
		}
		result.Samples[i] = int16(value)
	}
	return result, nil
}
