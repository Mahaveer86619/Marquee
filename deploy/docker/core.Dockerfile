# syntax=docker/dockerfile:1

FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X marquee/internal/version.Version=${VERSION} -X marquee/internal/version.Commit=${COMMIT}" \
      -o /out/marquee-core ./cmd/core

FROM debian:stable-slim
# ffmpeg/ffprobe are used for probing, remuxing and audio-only conversion.
# Installed from Debian packages; see THIRD_PARTY.md for license notes.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --no-create-home marquee \
 && mkdir -p /data /cache /downloads /library \
 && chown marquee:marquee /data /cache /downloads /library
COPY --from=build /out/marquee-core /usr/local/bin/marquee-core
USER marquee
EXPOSE 7700
ENTRYPOINT ["/usr/local/bin/marquee-core"]
