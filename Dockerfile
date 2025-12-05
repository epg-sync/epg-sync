FROM golang:1.24-alpine AS backend-builder

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o epg-sync ./cmd/server/main.go


FROM node:22-alpine AS frontend-builder

WORKDIR /web

RUN npm install -g pnpm

COPY web/package.json web/pnpm-lock.yaml ./
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

VOLUME ["/config", "/logs"]

ENV CONFIG_PATH=/config/config.yaml

ENTRYPOINT ["/app/entrypoint.sh"]