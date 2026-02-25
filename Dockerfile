# Multi-stage build for Go
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy source code
COPY . .

# Download dependencies and generate go.sum
RUN go mod tidy

# Build the application
# CGO_ENABLED=0 creates a statically linked binary
# -ldflags="-s -w" strips debug information to reduce binary size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o cfguard .

# Final stage
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies
# ca-certificates for HTTPS (Cloudflare API)
# tzdata for Timezone
# iputils for ping command
RUN apk add --no-cache ca-certificates tzdata iputils

# Copy binary from builder
COPY --from=builder /app/cfguard .

# Copy example config as default config
COPY --from=builder /app/config.example.yaml ./config.yaml

# Create instance directory for DB
RUN mkdir -p instance

# Set environment variables
ENV TZ=Asia/Shanghai

# Expose port
EXPOSE 8099

# Run the binary
CMD ["./cfguard"]
