package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

type Stream struct {
	Rate     uint32
	Channels uint16
	Samples  []int16
}

// decodeSamples returns a slice owned by the caller. Decoded samples are
// never drawn from shared buffers: concurrent decodes and successive tracks
// of one mix must not be able to overwrite each other's audio.
func decodeSamples(pcm []byte) []int16 {
	samples := make([]int16, len(pcm)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	return samples
}

func (s Stream) Frames() int { return len(s.Samples) / int(s.Channels) }

func Decode(b []byte) (Stream, error) {
	var s Stream
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return s, errors.New("invalid RIFF/WAVE header")
	}
	size := uint64(binary.LittleEndian.Uint32(b[4:8])) + 8
	if size != uint64(len(b)) {
		return s, errors.New("RIFF size does not match body")
	}
	var format, data bool
	var pcm []byte
	for p := 12; p < len(b); {
		if len(b)-p < 8 {
			return s, errors.New("truncated chunk header")
		}
		n := uint64(binary.LittleEndian.Uint32(b[p+4 : p+8]))
		start := p + 8
		if n > uint64(len(b)-start) {
			return s, errors.New("truncated chunk")
		}
		end := start + int(n)
		switch string(b[p : p+4]) {
		case "fmt ":
			if format || n < 16 {
				return s, errors.New("invalid format chunk")
			}
			format = true
			f := b[start:end]
			encoding := binary.LittleEndian.Uint16(f)
			s.Channels = binary.LittleEndian.Uint16(f[2:])
			s.Rate = binary.LittleEndian.Uint32(f[4:])
			byteRate := binary.LittleEndian.Uint32(f[8:])
			align := binary.LittleEndian.Uint16(f[12:])
			bits := binary.LittleEndian.Uint16(f[14:])
			if encoding != 1 || (s.Channels != 1 && s.Channels != 2) || bits != 16 || s.Rate == 0 || align != s.Channels*2 || uint64(byteRate) != uint64(s.Rate)*uint64(align) {
				return Stream{}, errors.New("unsupported or inconsistent PCM format")
			}
		case "data":
			if data {
				return s, errors.New("duplicate data chunk")
			}
			data = true
			pcm = b[start:end]
		}
		p = end
		if n%2 != 0 {
			if p >= len(b) {
				return s, errors.New("missing chunk padding")
			}
			p++
		}
	}
	if !format || !data || len(pcm)%int(s.Channels*2) != 0 {
		return Stream{}, errors.New("missing format or misaligned audio data")
	}
	s.Samples = decodeSamples(pcm)
	return s, nil
}

func Encode(s Stream) ([]byte, error) {
	if (s.Channels != 1 && s.Channels != 2) || s.Rate == 0 || len(s.Samples)%int(s.Channels) != 0 || uint64(s.Rate)*uint64(s.Channels)*2 > math.MaxUint32 || uint64(len(s.Samples))*2 > math.MaxUint32-36 {
		return nil, fmt.Errorf("invalid PCM stream")
	}
	b := make([]byte, 44+len(s.Samples)*2)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], s.Channels)
	binary.LittleEndian.PutUint32(b[24:], s.Rate)
	binary.LittleEndian.PutUint32(b[28:], s.Rate*uint32(s.Channels)*2)
	binary.LittleEndian.PutUint16(b[32:], s.Channels*2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(b)-44))
	for i, v := range s.Samples {
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(v))
	}
	return b, nil
}
