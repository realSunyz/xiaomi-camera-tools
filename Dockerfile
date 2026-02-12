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
    XIAOMI_VIDEO_CRON="0 10 * * *"

WORKDIR /work
RUN mkdir -p /data/input /data/output
COPY --from=builder /out/xiaomi-video /usr/local/bin/xiaomi-video

ENTRYPOINT ["/usr/local/bin/xiaomi-video"]
