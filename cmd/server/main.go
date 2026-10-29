package main

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/quic-go/quic-go"
	"quicadd/internal/protocol"
	"quicadd/internal/relay"
)

//go:embed web/*
var webFS embed.FS

var rm = relay.NewManager()
var authToken = os.Getenv("QUIC_TOKEN")

func main() {
	// 1. Setup QUIC Server
	tlsConfig := generateTLSConfig()
	quicConfig := &quic.Config{
		MaxIdleTimeout: 0, // Disable idle timeout for long waiting times
		KeepAlivePeriod: 0,
	}

	listener, err := quic.ListenAddr("0.0.0.0:4433", tlsConfig, quicConfig)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	log.Printf("[INFO] quicadd Direct Relay server listening on UDP :4433")

	// 2. Setup Web Server
	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("Failed to initialize web filesystem: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(subFS)))
	mux.HandleFunc("/api/files", apiAuth(handleWebList))
	mux.HandleFunc("/api/upload", apiAuth(handleWebUpload))
	mux.HandleFunc("/api/download/", apiAuth(handleWebDownload))

	srv := &http.Server{
		Addr:    ":8443",
		Handler: securityHeaders(mux),
	}

	go func() {
		log.Printf("[INFO] quicadd Web server listening on https://localhost:8443")
		err := srv.ListenAndServeTLS("certs/server.crt", "certs/server.key")
		if err != nil {
			log.Fatalf("Web server failed: %v", err)
		}
	}()

	// 3. Accept QUIC Connections
	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			log.Printf("[ERROR] Accept connection failed: %v", err)
			continue
		}
		log.Printf("[INFO] QUIC client connected: %s", conn.RemoteAddr().String())
		go handleConnection(conn)
	}
}

func apiAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		token := r.URL.Query().Get("token")
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if token != authToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data: https://api.qrserver.com; connect-src 'self';")
		next.ServeHTTP(w, r)
	})
}

func sanitizeFilename(name string) string {
	clean := filepath.Base(filepath.Clean(name))
	clean = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, clean)
	clean = strings.TrimSpace(clean)
	if clean == "" || clean == "." {
		return "download"
	}
	return clean
}

func handleWebUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Protect against unbounded uploads (max 5 GB)
	r.Body = http.MaxBytesReader(w, r.Body, 5<<30)

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Invalid or oversized file (max 5GB)", http.StatusBadRequest)
		return
	}
	defer file.Close()

	safeFilename := sanitizeFilename(header.Filename)
	log.Printf("[INFO] Web upload started (waiting for downloader): %s (%d bytes)", safeFilename, header.Size)

	// Block here until a downloader joins
	err = rm.Host(safeFilename, uint64(header.Size), file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[INFO] Web upload completed: %s", safeFilename)
	w.WriteHeader(http.StatusOK)
}

func handleWebDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawFilename := strings.TrimPrefix(r.URL.Path, "/api/download/")
	filename := sanitizeFilename(rawFilename)
	if filename == "" {
		http.Error(w, "Missing filename", http.StatusBadRequest)
		return
	}

	filesize, err := rm.GetFileSize(filename)
	if err != nil {
		http.Error(w, "No active upload for this file", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatUint(filesize, 10))

	log.Printf("[INFO] Web download joined: %s", filename)
	
	// Join the relay (copies from uploader to w)
	_, err = rm.Join(filename, w)
	if err != nil {
		log.Printf("[ERROR] Web download failed: %v", err)
		return
	}
	
	log.Printf("[INFO] Web download completed: %s", filename)
}

func handleWebList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rm.List()); err != nil {
		log.Printf("[ERROR] Failed to encode active sessions: %v", err)
	}
}

func handleConnection(conn *quic.Conn) {
	defer conn.CloseWithError(0, "closed")

	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			log.Printf("[INFO] QUIC client disconnected: %s", conn.RemoteAddr().String())
			return
		}
		go handleStream(stream)
	}
}

func handleStream(stream *quic.Stream) {
	defer stream.Close()

	header, err := protocol.ReadHeader(stream)
	if err != nil {
		log.Printf("[WARN] Failed to read header: %v", err)
		protocol.WriteResponse(stream, protocol.StatusError, "Invalid header format")
		return
	}

	if authToken != "" && header.Token != authToken {
		log.Printf("[WARN] Unauthorized attempt from client")
		protocol.WriteResponse(stream, protocol.StatusError, "Unauthorized")
		return
	}

	header.Filename = sanitizeFilename(header.Filename)

	switch header.Command {
	case protocol.CmdUpload:
		handleQUICUpload(stream, &header)
	case protocol.CmdDownload:
		handleQUICDownload(stream, &header)
	default:
		log.Printf("[WARN] Unknown command: %d", header.Command)
		protocol.WriteResponse(stream, protocol.StatusError, "Unknown command")
	}
}

func handleQUICUpload(stream *quic.Stream, header *protocol.Header) {
	log.Printf("[INFO] Upload started (waiting for downloader): %s (%d bytes)", header.Filename, header.FileSize)
	protocol.WriteResponse(stream, protocol.StatusOK, "Waiting for downloader...")

	// The uploader writes data, we read from stream and pass to the host session
	reader := io.LimitReader(stream, int64(header.FileSize))
	
	err := rm.Host(header.Filename, header.FileSize, reader)
	if err != nil {
		log.Printf("[ERROR] Upload failed: %s — %v", header.Filename, err)
		protocol.WriteResponse(stream, protocol.StatusError, err.Error())
		return
	}

	// Read 32-byte hash (optional to consume, we aren't storing it)
	clientHash := make([]byte, 32)
	io.ReadFull(stream, clientHash)

	msg := fmt.Sprintf("OK: %s [Relay Completed]", header.Filename)
	log.Printf("[INFO] Upload completed: %s", header.Filename)
	protocol.WriteResponse(stream, protocol.StatusOK, msg)
}

func handleQUICDownload(stream *quic.Stream, header *protocol.Header) {
	log.Printf("[INFO] Download requested: %s", header.Filename)

	filesize, err := rm.GetFileSize(header.Filename)
	if err != nil {
		protocol.WriteResponse(stream, protocol.StatusError, "No active upload found for this filename")
		return
	}

	// Send file size back
	protocol.WriteResponse(stream, protocol.StatusOK, fmt.Sprintf("%d", filesize))

	// We'll write to the stream
	written, err := rm.Join(header.Filename, stream)
	if err != nil {
		log.Printf("[ERROR] Download transfer failed: %s — %v", header.Filename, err)
		return
	}
	
	log.Printf("[INFO] Download completed: %s (%d bytes)", header.Filename, written)
}

func formatSize(bytes uint64) string {
	if bytes == 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	size := float64(bytes)
	for size >= 1024 && i < len(units)-1 {
		size /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", size, units[i])
}

func generateTLSConfig() *tls.Config {
	certPath := filepath.Join("certs", "server.crt")
	keyPath := filepath.Join("certs", "server.key")
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatalf("Failed to load TLS certs: %v. Run 'bash scripts/generate-certs.sh' first.", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quicadd", "quic-transfer"},
	}
}
