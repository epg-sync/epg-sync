# 多架构构建（amd64 / arm64）时，编译阶段都固定在构建机的原生平台上运行，
# 只有最后的运行镜像按目标架构构建。否则 arm64 的 npm / go build 要在 QEMU 里模拟执行，
# 慢一个数量级（实测 go build 307s vs 59s，npm install 长时间无响应）。

# Go 在原生平台上交叉编译出目标架构的二进制，CGO_ENABLED=0 不需要额外的工具链
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS backend-builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o epg-sync ./cmd/server/main.go


# 前端是静态导出（next.config 的 output: 'export'），产物与 CPU 架构无关，
# 在原生平台上只构建一次，所有目标架构共用
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder

WORKDIR /web

RUN npm install -g pnpm

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ .

ENV NEXT_PUBLIC_API_URL=""

RUN pnpm build


FROM alpine:latest

RUN apk add --no-cache nginx ca-certificates tzdata

WORKDIR /app

COPY --from=backend-builder /app/epg-sync /app/epg-sync

COPY --from=frontend-builder /web/out /app/public

COPY deploy/nginx.conf /etc/nginx/nginx.conf

COPY deploy/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 3000

VOLUME ["/app/config", "/app/logs"]

ENV APP_ENV=prod

ENTRYPOINT ["/app/entrypoint.sh"]
