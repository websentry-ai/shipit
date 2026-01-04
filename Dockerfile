# Frontend build stage
FROM node:20-alpine AS frontend

WORKDIR /app/web

# Copy package files for caching
COPY web/package*.json ./
RUN npm ci

# Copy frontend source and build
COPY web/ ./
RUN npm run build

# Go build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install git for go modules
RUN apk add --no-cache git ca-certificates

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy frontend build to embed location
COPY --from=frontend /app/web/dist ./internal/web/dist

# Build the server binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o shipit-server ./cmd/server

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install ca-certificates for HTTPS and aws-cli for EKS auth
RUN apk add --no-cache ca-certificates aws-cli

# Copy binary from builder
COPY --from=builder /app/shipit-server .

# Copy migrations for init
COPY migrations ./migrations

# Non-root user for security
RUN adduser -D -u 1000 shipit
USER shipit

EXPOSE 8090

ENTRYPOINT ["./shipit-server"]
