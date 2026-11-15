package audio

import (
	"audio-processing-service/internal/wav"
	"errors"
	"math"
)

func Clip(s wav.Stream, start, end int) (wav.Stream, error) {
	if start < 0 || end <= start || end > s.Frames() {
		return wav.Stream{}, errors.New("invalid frame interval")
	}
	s.Samples = append([]int16(nil), s.Samples[start*int(s.Channels):end*int(s.Channels)]...)
	return s, nil
}
func Gain(s wav.Stream, factor float64) (wav.Stream, error) {
	if math.IsNaN(factor) || math.IsInf(factor, 0) || factor < 0 || factor > 4 {
		return wav.Stream{}, errors.New("factor must be between 0 and 4")
	}
	samples := make([]int16, len(s.Samples))
	for i, v := range s.Samples {
		n := math.Round(float64(v) * factor)
		if n > 32767 {
			n = 32767
		}
		if n < -32768 {
			n = -32768
		}
		samples[i] = int16(n)
	}
	s.Samples = samples
	return s, nil
}
