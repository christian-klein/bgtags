# Multi-stage Dockerfile for bgtags
# Stage 1: Build statically linked binary
FROM golang:alpine AS builder
WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/bgtags ./cmd/server

# Stage 2: Minimal runtime container
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata wget qpdf

WORKDIR /app

COPY --from=builder /build/bgtags /app/bgtags
COPY templates /app/templates
COPY static /app/static
COPY data /app/data

RUN mkdir -p /app/data /app/backups

ENV PORT=8080 \
    DB_PATH=/app/data/bgtags.db \
    DATA_DIR=/app/data \
    STATIC_DIR=/app/static \
    BACKUP_ENABLED=true \
    BACKUP_DIR=/app/backups \
    BACKUP_TIME=00:00 \
    BACKUP_RETENTION=30

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["/app/bgtags"]
