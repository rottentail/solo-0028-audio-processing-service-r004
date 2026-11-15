package httpapi

import (
	"audio-processing-service/internal/audio"
	"audio-processing-service/internal/mix"
	"audio-processing-service/internal/wav"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

const maxBody = 16 << 20

func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		w.WriteHeader(200)
		fmt.Fprintln(w, "ok")
	})
	for _, path := range []string{"inspect", "clip", "gain"} {
		action := path
		mux.HandleFunc("/v1/"+path, func(w http.ResponseWriter, r *http.Request) { handle(w, r, action) })
	}
	mux.HandleFunc("/v1/mix", handleMix)
	return mux
}

func handleMix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, mix.MaxRequestBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "cannot read body", http.StatusBadRequest)
		}
		return
	}
	output, err := mix.Render(r.Context(), body)
	if err != nil {
		if r.Context().Err() == nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	_, _ = w.Write(output)
}
func handle(w http.ResponseWriter, r *http.Request, action string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "body too large", 413)
		} else {
			http.Error(w, "cannot read body", 400)
		}
		return
	}
	s, err := wav.Decode(b)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if action == "inspect" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"sample_rate": s.Rate, "channels": s.Channels, "bits_per_sample": 16, "frames": s.Frames(), "duration_seconds": float64(s.Frames()) / float64(s.Rate)})
		return
	}
	switch action {
	case "clip":
		start, e1 := strconv.Atoi(r.URL.Query().Get("start"))
		end, e2 := strconv.Atoi(r.URL.Query().Get("end"))
		if e1 != nil || e2 != nil {
			http.Error(w, "start and end must be integers", 400)
			return
		}
		s, err = audio.Clip(s, start, end)
	case "gain":
		factor, e := strconv.ParseFloat(r.URL.Query().Get("factor"), 64)
		if e != nil {
			http.Error(w, "factor must be numeric", 400)
			return
		}
		s, err = audio.Gain(s, factor)
	}
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	out, err := wav.Encode(s)
	if err != nil {
		http.Error(w, "encoding failed", 500)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Write(out)
}
