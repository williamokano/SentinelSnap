FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /sentinelsnap ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates curl \
    && adduser -D -u 1000 appuser
WORKDIR /app
COPY --from=builder /sentinelsnap .
# Pre-create the in-container LOCAL_UPLOAD_DIR so the non-root user can write
# to it (named volumes inherit this ownership on first use).
RUN mkdir -p /app/uploads && chown -R appuser:appuser /app
EXPOSE 8080
USER appuser
ENTRYPOINT ["/app/sentinelsnap"]
