package profile

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestCanvasPreservesDisplayAspect(t *testing.T) {
	ffmpeg := os.Getenv("FFMPEG_PATH")
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	if _, err := exec.LookPath(ffmpeg); err != nil {
		t.Skip("FFmpeg is not available")
	}
	for _, tc := range []struct {
		name, source, sar string
		width, height     int
	}{
		{"wide pixels", "320x240", "2/1", 640, 240},
		{"portrait", "240x320", "1/1", 270, 360},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := (Settings{Width: 640, Height: 360}).VideoArgs()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=white:s="+tc.source+":r=10", "-frames:v", "1", "-vf", "setsar="+tc.sar+","+filter[1], "-f", "rawvideo", "-pix_fmt", "gray", "-")
			pixels, err := cmd.Output()
			if err != nil {
				t.Fatalf("FFmpeg: %v", err)
			}
			if len(pixels) != 640*360 {
				t.Fatalf("unexpected frame size %d", len(pixels))
			}
			minX, minY, maxX, maxY := 640, 360, -1, -1
			for y := 0; y < 360; y++ {
				for x := 0; x < 640; x++ {
					if pixels[y*640+x] < 180 {
						continue
					}
					if x < minX {
						minX = x
					}
					if x > maxX {
						maxX = x
					}
					if y < minY {
						minY = y
					}
					if y > maxY {
						maxY = y
					}
				}
			}
			width, height := maxX-minX+1, maxY-minY+1
			if width != tc.width || height != tc.height {
				t.Fatalf("content bounds %dx%d, want %dx%d", width, height, tc.width, tc.height)
			}
		})
	}
}
