# Audio Processing Service

An HTTP service for media engineers to inspect and transform uncompressed PCM WAV audio and run asynchronous video and audio conversions. Immediate WAV operations do not retain request bodies; media jobs retain input, state and output in a private service data directory.

## Entities and rules

An audio stream has a sample rate, channel count, bit depth, frame count and interleaved PCM samples. Only integer PCM, mono or stereo, 16-bit samples are accepted. RIFF chunk lengths and frame alignment must be valid. A clip uses a half-open frame interval within the stream. Gain is a finite multiplier from 0 through 4 with saturated samples.

## Workflows

1. Inspect: decode an uploaded WAV and return metadata as JSON.
2. Clip: validate a frame interval, slice full frames and encode the result as WAV.
3. Gain: validate a multiplier, scale and saturate samples, and encode a WAV result.
4. Transcode video to MP4, extract an M4A audio track, or extract a JPEG frame through an asynchronous job. A bounded queue and two workers limit concurrent FFmpeg processes. Jobs may be cancelled while queued or running; failed or cancelled jobs may be retried after any running process exits. A restarted service marks unfinished jobs failed and retains their input for explicit retry.
5. Submit a video job with a paired start and duration and/or even canvas dimensions. The selected time applies to both audio and video. Video frames are scaled without distortion and padded to the requested canvas. Parameters are fixed in the job record across retries. Results are decoded before they become available.
6. Mix 1 to 16 WAV tracks through a synchronous endpoint or persistent `mix-wav` job. Tracks share sample rate and channel count, are aligned at frame offsets, and use individual gains. Overlaps sum before saturation; gaps are silent. Both paths apply the same request and output limits and produce the same PCM WAV.

## Modules and interfaces

`cmd/audioserve` starts the server; `internal/httpapi` validates HTTP inputs and maps errors; `internal/wav` decodes and encodes RIFF PCM; `internal/audio` implements frame operations. POST `/v1/inspect`, `/v1/clip?start=0&end=1`, and `/v1/gain?factor=1.5` accept WAV bodies. A GET `/healthz` indicates process availability.

`internal/jobs` owns durable job records, queue admission, process cancellation and output publication. POST `/v1/jobs?operation=video-mp4`, `audio-m4a`, `thumbnail`, or `mix-wav` submits input. POST `/v1/mix` synchronously renders a JSON mix request; `mix-wav` accepts the same JSON asynchronously. GET `/v1/jobs` lists records; GET `/v1/jobs/{id}` reads status; GET `/v1/jobs/{id}/output` retrieves completed output; DELETE cancels; POST `/v1/jobs/{id}/retry` retries. Each data directory must be used by only one service process.
`internal/profile` validates paired temporal and spatial parameters and constructs the corresponding FFmpeg arguments.

## Validation plan

Build the executable and exercise each endpoint against generated PCM audio and FFmpeg-generated video using bounded offline smoke checks, including invalid input, retry, restart and transformed output checks. `go test ./...` covers mixing and its synchronous and persistent job paths; additional fuzz and crash-recovery coverage remains useful.
