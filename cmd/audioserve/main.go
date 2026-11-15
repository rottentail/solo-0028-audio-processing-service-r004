package main

import (
	"audio-processing-service/internal/httpapi"
	"audio-processing-service/internal/jobs"
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := os.Getenv("AUDIO_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	root := os.Getenv("AUDIO_DATA_DIR")
	if root == "" {
		root = "./audio-data"
	}
	ffmpeg := os.Getenv("FFMPEG_PATH")
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	manager, err := jobs.Open(root, ffmpeg, 2, 32)
	if err != nil {
		log.Fatal(err)
	}
	defer manager.Close()
	mux := http.NewServeMux()
	httpapi.RegisterJobs(mux, manager)
	mux.Handle("/", httpapi.Handler())
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	log.Printf("listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
