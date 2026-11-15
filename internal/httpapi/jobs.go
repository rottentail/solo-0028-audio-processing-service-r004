package httpapi

import (
	"audio-processing-service/internal/jobs"
	"audio-processing-service/internal/profile"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func RegisterJobs(mux *http.ServeMux, manager *jobs.Manager) {
	mux.HandleFunc("/v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, 200, manager.List())
		case http.MethodPost:
			settings, err := profile.Parse(r.URL.Query().Get("operation"), r.URL.Query())
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, (64<<20)+1)
			j, err := manager.Create(r.URL.Query().Get("operation"), settings, r.Body)
			if err != nil {
				jobError(w, err)
				return
			}
			writeJSON(w, 202, j)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})
	mux.HandleFunc("/v1/jobs/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/jobs/"), "/")
		id := parts[0]
		if id == "" || len(parts) > 2 {
			http.NotFound(w, r)
			return
		}
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				j, err := manager.Get(id)
				if err != nil {
					jobError(w, err)
					return
				}
				writeJSON(w, 200, j)
			case http.MethodDelete:
				if err := manager.Cancel(id); err != nil {
					jobError(w, err)
					return
				}
				w.WriteHeader(204)
			default:
				http.Error(w, "method not allowed", 405)
			}
			return
		}
		if parts[1] == "output" && r.Method == http.MethodGet {
			f, mime, err := manager.Output(id)
			if err != nil {
				jobError(w, err)
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", mime)
			_, _ = io.Copy(w, f)
			return
		}
		if parts[1] == "retry" && r.Method == http.MethodPost {
			j, err := manager.Retry(id)
			if err != nil {
				jobError(w, err)
				return
			}
			writeJSON(w, 202, j)
			return
		}
		http.NotFound(w, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func jobError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jobs.ErrNotFound):
		http.Error(w, err.Error(), 404)
	case errors.Is(err, jobs.ErrConflict):
		http.Error(w, err.Error(), 409)
	case errors.Is(err, jobs.ErrFull):
		http.Error(w, err.Error(), 503)
	case errors.Is(err, jobs.ErrInvalid):
		http.Error(w, err.Error(), 400)
	default:
		http.Error(w, "job storage or request read failed", 500)
	}
}
