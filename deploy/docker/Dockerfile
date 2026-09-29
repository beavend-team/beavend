# Multi-stage Docker build for Beavend
FROM golang:1.22-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git ca-certificates

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code (including embedded dashboard in dashboard/dist)
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY dashboard/ ./dashboard/

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /beavend ./cmd/beavend

# Runtime image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /beavend /app/beavend

EXPOSE 8080

ENTRYPOINT ["/app/beavend"]
CMD ["--role=all"]
