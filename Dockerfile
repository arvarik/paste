# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download || true

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o paste-app .

# Runtime stage
FROM alpine:3.20

LABEL org.opencontainers.image.source="https://github.com/paste"
LABEL org.opencontainers.image.description="Lightweight, self-hosted pastebin"
LABEL org.opencontainers.image.licenses="MIT"

WORKDIR /app
COPY --from=builder /app/paste-app /app/
COPY templates /app/templates/

RUN mkdir -p /app/data && chown -R 3000:3000 /app

EXPOSE 8083
ENTRYPOINT ["/app/paste-app"]
