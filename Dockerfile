FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY . .

ENV CGO_ENABLED=0
ARG MAIN_PKG=./src
RUN go build -trimpath -ldflags="-s -w" -o /out/xiaomi-video ${MAIN_PKG}

FROM alpine:3.20
RUN apk add --no-cache ffmpeg tzdata ca-certificates && update-ca-certificates

ENV TZ=Asia/Shanghai \
    XIAOMI_VIDEO_DIR=/data/input \
    XIAOMI_VIDEO_OUT_DIR=/data/output \
    XIAOMI_VIDEO_OUT_EXT=.mp4 \
    XIAOMI_VIDEO_MERGE=true \
    XIAOMI_VIDEO_CLEANUP=true \
    XIAOMI_VIDEO_DAYS=30 \
    XIAOMI_VIDEO_OVERWRITE=false \
    XIAOMI_VIDEO_DRY_RUN=false \
    XIAOMI_VIDEO_VERBOSE=true \
    XIAOMI_VIDEO_GENPTS=false \
    XIAOMI_VIDEO_DELETE_SEGMENTS=true

WORKDIR /work
RUN mkdir -p /data/input /data/output
COPY --from=builder /out/xiaomi-video /usr/local/bin/xiaomi-video

ENTRYPOINT ["/usr/local/bin/xiaomi-video"]
