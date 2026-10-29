# quicadd ⚡

> **High-Performance, Zero-Storage Ephemeral File Relay**  
> Powered by QUIC (UDP) protocol, HTTP/2 REST API, and a modern Obsidian glassmorphic Web UI.

---

## 🚀 Key Highlights

- **Zero Server Storage**: Files **never touch the server's hard drive**. Streaming data is piped memory-to-memory (`io.Copy` in 32 KB chunks) directly between the uploader and the downloader.
- **QUIC Protocol (UDP :4433)**: Built with [`github.com/quic-go/quic-go`](https://github.com/quic-go/quic-go) for minimal latency, 0-RTT handshakes, and immunity to head-of-line blocking.
- **Modern Obsidian Glassmorphic Web UI (TCP :8443)**: Ambient radial glowing mesh, drag-and-drop file portal, animated radar pulses, live percentage/speed indicators, one-click copyable share links (`?file=...`), and instant mobile QR codes.
- **Enterprise Security & IP Masking**:
  - Origin server IP and ports are shielded behind a Cloudflare Tunnel.
  - Hardened with security headers (`Content-Security-Policy`, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`).
  - Strict path traversal sanitization and 5 GB maximum upload protection.
  - 15-minute auto-cleanup timeout prevents stale memory leaks.

---

## 🛠️ Quick Start

### 1. Prerequisites & Certificates
QUIC mandates TLS 1.3 encryption. Generate local certificates:
```bash
bash scripts/generate-certs.sh
```

### 2. Build Binaries
```bash
# Build the server (embeds Web UI into the binary)
go build -o quicadd-server ./cmd/server

# Build the CLI client
go build -o quicadd ./cmd/client
```

### 3. Run the Server
```bash
./quicadd-server
```
The server listens on:
- **`UDP :4433`**: Native QUIC protocol for high-speed CLI transfers.
- **`TCP :8443`**: HTTPS Web UI & streaming REST endpoints.

---

## 💻 CLI Client Usage (`quicadd`)

The `quicadd` CLI client can host or join ephemeral file transfers across local or remote instances:

```bash
# Host a file for direct relay
./quicadd upload sample.iso

# In another terminal / machine: join the relay and download
./quicadd download sample.iso

# Download to a custom destination or filename
./quicadd download sample.iso /tmp/my_downloaded_file.iso

# Connect to a remote quicadd relay server
./quicadd -server your-server-ip:4433 download dataset.tar.gz
```

---

## 🌐 Web Interface

Navigate to `https://localhost:8443` (or your public Cloudflare Tunnel URL):

1. **Host a File**: Drag and drop any file into the portal and click **Start Transmission**.
2. **Share Link / QR Code**: Copy the auto-generated share URL (`?file=...`) or display the instant QR code for mobile devices.
3. **Download**: As soon as the recipient opens the link or clicks **Download**, the transfer begins streaming directly through memory. Once complete, the session automatically terminates.

---

## 🐳 Docker Deployment

```bash
docker build -t quicadd .
docker run -p 4433:4433/udp -p 8443:8443/tcp quicadd
```

---

## 📁 Repository Structure

```text
quic/
├── cmd/
│   ├── client/
│   │   └── main.go           # quicadd CLI client (upload & download)
│   └── server/
│       ├── main.go           # quicadd relay server (QUIC + Web UI)
│       └── web/              # Embedded SPA (HTML, CSS, JS)
├── internal/
│   ├── protocol/
│   │   └── protocol.go       # Binary wire protocol specification
│   └── relay/
│       └── relay.go          # In-memory streaming relay manager
├── certs/                    # TLS certificates
├── scripts/
│   └── generate-certs.sh     # TLS 1.3 self-signed cert generator
├── Dockerfile                # Multi-stage production container build
├── go.mod                    # Module definition (quicadd)
└── README.md
```

---

## 📄 License

MIT
