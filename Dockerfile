# syntax=docker/dockerfile:1

# 阶段一：构建前端静态资源（产物嵌入 Go 二进制）
FROM docker.io/oven/bun:1 AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run generate

# 阶段二：编译 Go 二进制（纯 Go，免 CGO）
FROM docker.io/library/golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/gzzchh/asu-go/internal/cli.Version=${VERSION}" -o /out/asu ./cmd/asu

# 阶段三：运行镜像
FROM docker.io/library/alpine:3
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/asu /usr/local/bin/asu
# 构建容器由宿主引擎创建，本服务需要访问引擎 socket，因此默认以 root 运行
WORKDIR /data
VOLUME ["/data"]
EXPOSE 8000
ENTRYPOINT ["asu"]
CMD ["serve", "--config", "/etc/asu/asu.yaml"]
