# Build stage
FROM golang:1.26.1-alpine AS builder

WORKDIR /app

# Install git for fetching dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o ollama-proxy .

# Final stage
FROM alpine:3.20

WORKDIR /app

# Install CA certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Copy the binary from builder stage
COPY --from=builder /app/ollama-proxy /app/ollama-proxy

# Expose the default port (adjust if your app uses a different port)
EXPOSE 11434

# Run the application
CMD ["./ollama-proxy", "-config", "/data/config.yaml"]