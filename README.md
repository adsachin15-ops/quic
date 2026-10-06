# QUIC File Transfer (Direct Relay)

A simple, memory-efficient QUIC-based file transfer application built with Go. 
This operates as a **Direct Relay** server — it pipes files instantly from Sender to Receiver entirely in memory. Files are **never** stored on the server's disk, meaning you can host this securely on ephemeral serverless platforms or low-storage VPS droplets!

## Features

- **Direct Peer-to-Peer Relay:** Streams files live from uploader to downloader.
- **Memory Efficient:** Uses `io.Copy` limits; handles 100GB+ files effortlessly with zero disk usage.
- **SHA-256 integrity:** Files are hashed end-to-end to verify integrity.
- **Authentication:** Secure your server with a simple `QUIC_TOKEN`.
- **Dockerized:** Ready for production deployment.

## Project Structure

```text
quic-transfer/
├── cmd/
│   ├── server/main.go         # QUIC Relay Server
│   └── client/main.go         # CLI client
├── internal/
│   ├── protocol/protocol.go   # Binary wire format
│   └── relay/relay.go         # In-memory stream matching
├── certs/                     # TLS certs
├── scripts/generate-certs.sh
└── Dockerfile
```

## Running the Server

Start the server, optionally securing it with a token:

```bash
export QUIC_TOKEN="my-secret-token"
./server
```

The server listens on **UDP :4433**.

## Using the Client

Because this is a live relay, both the sender and receiver must be online.

**1. Sender starts upload (and waits):**
```bash
export QUIC_TOKEN="my-secret-token"
./client upload large_file.bin
```

**2. Receiver starts download (starts transfer instantly):**
```bash
export QUIC_TOKEN="my-secret-token"
./client download large_file.bin
```

## Running with Docker

```bash
docker build -t quic-transfer .
docker run -p 4433:4433/udp -e QUIC_TOKEN="my-secret" quic-transfer
```

## License

MIT
