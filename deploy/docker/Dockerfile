# Frontend build stage
FROM node:20-alpine AS frontend-builder
WORKDIR /app/dashboard
COPY dashboard/package*.json ./
RUN npm ci
COPY dashboard/ ./
RUN npm run build

# Go build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app
RUN apk add --no-cache git ca-certificates

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and compiled dashboard dist
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY dashboard/dashboard.go ./dashboard/dashboard.go
COPY --from=frontend-builder /app/dashboard/dist ./dashboard/dist

# Build static binary with embedded dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /beavend ./cmd/beavend

# Runtime image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app

COPY --from=builder /beavend /app/beavend

EXPOSE 8080

ENTRYPOINT ["/app/beavend"]
CMD ["--role=all"]
