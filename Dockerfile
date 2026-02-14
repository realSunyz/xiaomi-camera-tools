FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY . .

ARG MAIN_PKG=./src
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/xiaomi-camera-tools ${MAIN_PKG}

FROM alpine:3.23
RUN apk add --no-cache ffmpeg tzdata ca-certificates && update-ca-certificates \
    && mkdir -p /data/input /data/output /work

ENV TZ=Asia/Shanghai \
    XIAOMI_VIDEO_DIR=/data/input \
    XIAOMI_VIDEO_OUT_DIR=/data/output \
    XIAOMI_VIDEO_CRON="0 8 * * *"

WORKDIR /work
COPY --from=builder /out/xiaomi-camera-tools /usr/local/bin/xiaomi-camera-tools

ENTRYPOINT ["/usr/local/bin/xiaomi-camera-tools"]
