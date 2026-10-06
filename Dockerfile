# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install dependencies (none extra needed, but download modules)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server (embeds web UI automatically)
RUN CGO_ENABLED=0 GOOS=linux go build -o quic-server ./cmd/server

# Final stage
FROM alpine:latest

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/quic-server .

# Copy TLS certs (in production, mount these via volume)
COPY --from=builder /app/certs ./certs

# Expose QUIC UDP port
EXPOSE 4433/udp

# Run the server
CMD ["./quic-server"]
