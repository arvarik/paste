# Frontend asset stage
FROM node:24.15.0-alpine@sha256:d1b3b4da11eefd5941e7f0b9cf17783fc99d9c6fc34884a665f40a06dbdfc94f AS assets

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts
COPY static ./static
COPY templates ./templates
RUN npm test && npm run build

# Go build stage
FROM golang:1.26.6-alpine3.24@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
ARG BUILD_VERSION
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.buildVersion=${BUILD_VERSION}" -o paste-app ./cmd/server/
RUN CGO_ENABLED=0 GOOS=linux go build -o pastectl ./cmd/pastectl/

# Runtime stage
FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

LABEL org.opencontainers.image.source="https://github.com/arvarik/paste"
LABEL org.opencontainers.image.description="Lightweight, self-hosted pastebin"
LABEL org.opencontainers.image.licenses="MIT"

WORKDIR /app

RUN addgroup -S -g 3000 paste \
    && adduser -S -D -H -u 3000 -G paste paste \
    && mkdir -p /app/data \
    && chown -R paste:paste /app

COPY --from=builder --chown=paste:paste /app/paste-app /app/
COPY --from=builder --chown=paste:paste /app/pastectl /app/
COPY --chown=paste:paste templates /app/templates/
COPY --from=assets --chown=paste:paste /app/static/dist /app/static/dist/

EXPOSE 8083
USER paste:paste
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD wget --spider -q http://127.0.0.1:8083/healthz || exit 1
ENTRYPOINT ["/app/paste-app"]
