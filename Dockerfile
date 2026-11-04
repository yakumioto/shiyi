# syntax=docker/dockerfile:1
#
# Shiyi —— 单用户、Chat-first 的本地个人记忆服务。
#
# 镜像分两段：
#   builder  编译 Go 二进制。依赖已经 vendor 进仓库，构建期不联网。
#   runtime  精简运行镜像，必须带 ripgrep（检索靠 `rg` 预筛），并以非 root 运行。
#
# 构建：
#   docker build -t shiyi:latest .
#
# 运行（数据挂到 /data，密钥只经环境变量传入）：
#   docker run -d --name shiyi -p 8787:8787 \
#     -v shiyi-data:/data \
#     -e SHIYI_API_KEY=sk-... \
#     shiyi:latest

# ---------------------------------------------------------------- builder

FROM golang:1.26-alpine AS builder

# 依赖已 vendor，构建期不需要网络；显式关掉代理，避免任何隐式下载。
ENV GOFLAGS=-mod=vendor \
    GOPROXY=off \
    GOSUMDB=off \
    CGO_ENABLED=0

WORKDIR /src

# 先只拷依赖清单，让依赖层可以独立缓存。
COPY go.mod go.sum ./
COPY vendor/ vendor/

# 再拷源码。
COPY cmd/ cmd/
COPY internal/ internal/

# -trimpath 去掉构建机路径，-ldflags 去掉符号表，减小体积并避免泄露路径。
RUN go build -trimpath -ldflags="-s -w" -o /out/shiyi ./cmd/shiyi

# ---------------------------------------------------------------- runtime

FROM alpine:3.22 AS runtime

# ripgrep 是检索的硬依赖；ca-certificates 供容器直连 OpenAI 兼容的 HTTPS 端点。
RUN apk add --no-cache ripgrep ca-certificates \
    && addgroup -g 1000 -S shiyi \
    && adduser -u 1000 -S -G shiyi -h /home/shiyi shiyi \
    && mkdir -p /data /etc/shiyi \
    && chown -R shiyi:shiyi /data /etc/shiyi

COPY --from=builder /out/shiyi /usr/local/bin/shiyi

# 镜像内置一份开箱即用的配置：数据目录指向数据卷，监听全部网卡，
# 密钥通过 SHIYI_API_KEY 环境变量提供（配置里只写变量名，不写密钥）。
COPY docs/config.docker.toml /etc/shiyi/config.toml

ENV SHIYI_DATA_DIR=/data \
    SHIYI_LISTEN=0.0.0.0:8787 \
    SHIYI_CONFIG=/etc/shiyi/config.toml

VOLUME /data
EXPOSE 8787

USER shiyi
WORKDIR /data

# 镜像里没有 curl，用 alpine 自带的 busybox wget 探活。
# wget 只有在 HTTP 2xx 时才返回 0，非 2xx 与连接失败都返回非 0。
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8787/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/shiyi"]
CMD ["serve"]
