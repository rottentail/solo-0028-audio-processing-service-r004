package profile

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
)

// Settings are persisted with a job and reused unchanged by each attempt.
type Settings struct {
	Start    float64 `json:"start_seconds,omitempty"`
	Duration float64 `json:"duration_seconds,omitempty"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
}

func Parse(operation string, q url.Values) (Settings, error) {
	var s Settings
	for key, values := range q {
		if len(values) != 1 {
			return s, fmt.Errorf("duplicate parameter %s", key)
		}
		switch key {
		case "operation", "start", "duration", "width", "height":
		default:
			return s, fmt.Errorf("unknown parameter %s", key)
		}
	}
	hasTrim := q.Has("start") || q.Has("duration")
	hasSize := q.Has("width") || q.Has("height")
	if (hasTrim || hasSize) && operation != "video-mp4" {
		return s, errors.New("video parameters require video-mp4")
	}
	if hasTrim {
		if !q.Has("start") || !q.Has("duration") {
			return s, errors.New("start and duration must be specified together")
		}
		var err error
		s.Start, err = strconv.ParseFloat(q.Get("start"), 64)
		if err != nil || math.IsNaN(s.Start) || math.IsInf(s.Start, 0) || s.Start < 0 || s.Start > 86400 {
			return s, errors.New("start must be finite and between 0 and 86400")
		}
		s.Duration, err = strconv.ParseFloat(q.Get("duration"), 64)
		if err != nil || math.IsNaN(s.Duration) || math.IsInf(s.Duration, 0) || s.Duration <= 0 || s.Duration > 3600 {
			return s, errors.New("duration must be finite and in (0, 3600]")
		}
	}
	if hasSize {
		if !q.Has("width") || !q.Has("height") {
			return s, errors.New("width and height must be specified together")
		}
		var err error
		s.Width, err = strconv.Atoi(q.Get("width"))
		if err != nil {
			return s, errors.New("invalid width")
		}
		s.Height, err = strconv.Atoi(q.Get("height"))
		if err != nil {
			return s, errors.New("invalid height")
		}
		if s.Width < 16 || s.Width > 1920 || s.Height < 16 || s.Height > 1080 || s.Width%2 != 0 || s.Height%2 != 0 {
			return s, errors.New("dimensions must be even and within 16x16 to 1920x1080")
		}
	}
	return s, nil
}

func (s Settings) VideoArgs() []string {
	var args []string
	if s.Duration > 0 {
		args = append(args, "-ss", strconv.FormatFloat(s.Start, 'f', -1, 64), "-t", strconv.FormatFloat(s.Duration, 'f', -1, 64))
	}
	if s.Width > 0 {
		args = append(args, "-vf", fmt.Sprintf("scale=w=max(2\\,trunc(min(%d\\,%d*dar)/2)*2):h=max(2\\,trunc(min(%d\\,%d/dar)/2)*2),setsar=1,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", s.Width, s.Height, s.Height, s.Width, s.Width, s.Height))
	}
	return args
}
