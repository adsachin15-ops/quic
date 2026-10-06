# QUIC File Transfer

A simple, practical QUIC-based file transfer application built with Go and quic-go.

## Quick Start

```bash
# 1. Generate TLS certificates (required — QUIC mandates TLS 1.3)
bash scripts/generate-certs.sh

# 2. Build
go build -o server ./cmd/server
go build -o client ./cmd/client

# 3. Run the server
./server

# 4. In another terminal, run the client
./client
```

## Features

- **Upload, Download, List, Delete** files using a lightweight binary protocol over QUIC.
- **Web UI** and REST API served concurrently on TCP :8443.
- **Resume support**: Interrupted uploads can be resumed seamlessly.
- **SHA-256 integrity**: Files are hashed during transfer to verify integrity.
- **Authentication**: Secure your server with a simple `QUIC_TOKEN`.
- **Security**: Built-in path traversal protection.
- **Dockerized**: Ready for production deployment.

## Project Structure

```text
quic-transfer/
├── cmd/
│   ├── server/
│   │   ├── main.go            # QUIC + HTTPS server
│   │   └── web/               # Embedded Web UI
│   └── client/
│       └── main.go            # CLI client
├── internal/
│   ├── protocol/protocol.go   # Binary wire format
│   └── filemanager/           # Safe file I/O
├── certs/                     # TLS certs
├── data/files/                # File storage
├── scripts/generate-certs.sh
└── Dockerfile
```

## Running the Server

Start the server, optionally securing it with a token:

```bash
export QUIC_TOKEN="my-secret-token"
./server
```

The server listens on:
- **UDP :4433** - QUIC binary protocol (for the CLI)
- **TCP :8443** - HTTPS Web UI & REST API

### Using the Web UI

Open your browser and navigate to: `https://localhost:8443`.
*Note: If using self-signed certificates, you will need to bypass your browser's security warning.*
If you started the server with `QUIC_TOKEN`, append it to the URL: `https://localhost:8443/?token=my-secret-token`

### Using the CLI Client

Set the token in your environment and use the client:

```bash
export QUIC_TOKEN="my-secret-token"

# Upload a file (supports resuming if interrupted!)
./client upload large_file.bin

# Download a file
./client download large_file.bin

# List files
./client list

# Delete a file
./client delete large_file.bin
```

## Running with Docker

```bash
docker build -t quic-transfer .
docker run -p 4433:4433/udp -p 8443:8443/tcp quic-transfer
```

## License

MIT
