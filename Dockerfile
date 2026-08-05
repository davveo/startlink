# ---- build ----
FROM golang:1.22-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates tzdata

ENV GOPROXY=https://goproxy.io,https://mirrors.aliyun.com/goproxy/,direct
ENV CGO_ENABLED=0
ENV GOOS=linux

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p /out \
 && go build -ldflags="-s -w" -o /out/api ./cmd/api \
 && go build -ldflags="-s -w" -o /out/scheduler ./cmd/scheduler \
 && go build -ldflags="-s -w" -o /out/pusher ./cmd/pusher

# ---- runtime ----
# 后端三进程共用此镜像（APP=api|scheduler|pusher）。
# 前端为独立镜像：见 web/Dockerfile（Compose 服务名 web，端口 3000）。
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata wget \
 && adduser -D -H -u 10001 starlink

COPY --from=builder /out/api /app/api
COPY --from=builder /out/scheduler /app/scheduler
COPY --from=builder /out/pusher /app/pusher
COPY configs/config.docker.yaml /app/configs/config.yaml
COPY scripts/docker-entrypoint.sh /app/docker-entrypoint.sh

RUN chmod +x /app/docker-entrypoint.sh \
 && chown -R starlink:starlink /app

USER starlink

ENV APP=api
ENV CONFIG=/app/configs/config.yaml
ENV TZ=Asia/Shanghai

EXPOSE 8080

ENTRYPOINT ["/app/docker-entrypoint.sh"]
