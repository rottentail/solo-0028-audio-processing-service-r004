FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/audioserve ./cmd/audioserve

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir /data && chown 10001:10001 /data
COPY --from=build /out/audioserve /usr/local/bin/audioserve
ENV AUDIO_LISTEN=0.0.0.0:8080 AUDIO_DATA_DIR=/data
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/audioserve"]
