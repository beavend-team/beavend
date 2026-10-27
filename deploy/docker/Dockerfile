# Build static binary with pre-compiled dashboard dist
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY dashboard/ ./dashboard/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /beavend ./cmd/beavend

# Runtime image using locally available alpine base
FROM golang:1.26-alpine

WORKDIR /app
COPY --from=builder /beavend /app/beavend

EXPOSE 8080

ENTRYPOINT ["/app/beavend"]
CMD ["--role=all"]
