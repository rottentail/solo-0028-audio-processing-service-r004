package mix

import (
	"audio-processing-service/internal/audio"
	"audio-processing-service/internal/wav"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

const MaxRequestBytes = 16 << 20

var encodedWorkspace = struct {
	sync.Mutex
	buffers map[int][]byte
}{buffers: make(map[int][]byte)}

func encodedResult(stream wav.Stream) ([]byte, error) {
	encoded, err := wav.Encode(stream)
	if err != nil || len(encoded) < 65536 {
		return encoded, err
	}
	size := 65536
	for size < len(encoded) {
		size *= 2
	}
	encodedWorkspace.Lock()
	defer encodedWorkspace.Unlock()
	buffer := encodedWorkspace.buffers[size]
	if len(buffer) < len(encoded) {
		buffer = make([]byte, size)
		encodedWorkspace.buffers[size] = buffer
	}
	result := buffer[:len(encoded)]
	copy(result, encoded)
	return result, nil
}

type request struct {
	Tracks []struct {
		Audio []byte   `json:"audio"`
		Start int      `json:"start"`
		Gain  *float64 `json:"gain"`
	} `json:"tracks"`
}

func Render(ctx context.Context, input []byte) ([]byte, error) {
	if len(input) == 0 || len(input) > MaxRequestBytes {
		return nil, errors.New("invalid mix request size")
	}
	var req request
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return nil, fmt.Errorf("invalid mix request: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("mix request must contain one JSON object")
	}
	if len(req.Tracks) == 0 || len(req.Tracks) > 16 {
		return nil, errors.New("mix requires between 1 and 16 tracks")
	}
	tracks := make([]audio.MixTrack, 0, len(req.Tracks))
	for i, item := range req.Tracks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		stream, err := wav.Decode(item.Audio)
		if err != nil {
			return nil, fmt.Errorf("track %d: %w", i+1, err)
		}
		gain := 1.0
		if item.Gain != nil {
			gain = *item.Gain
		}
		tracks = append(tracks, audio.MixTrack{Stream: stream, Start: item.Start, Gain: gain})
	}
	result, err := audio.Mix(ctx, tracks)
	if err != nil {
		return nil, err
	}
	return encodedResult(result)
}
