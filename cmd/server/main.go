// Phase 5: QUIC file transfer server with Web UI
//
// This server runs two listeners concurrently:
//
//   UDP :4433  →  QUIC binary protocol (for CLI client)
//   TCP :8443  →  HTTPS web server (for browser)
//
// Both share the same FileManager, so files uploaded via CLI
// appear in the web UI and vice versa.
//
// Why two ports?
//   - Browsers speak HTTP/3 (ALPN "h3") or HTTPS — they can't use
//     a custom binary protocol directly.
//   - The CLI uses our lightweight binary protocol over QUIC for
//     efficient file transfer with progress tracking.
//   - Both use TLS (QUIC mandates it, HTTPS uses it on TCP).
//
// Architecture:
//
//   Browser ──HTTPS──→ TCP :8443 ──→ REST API ──→ FileManager ──→ data/files/
//   CLI    ──QUIC───→ UDP :4433 ──→ Protocol  ──→ FileManager ──→ data/files/

package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/quic-go/quic-go"

	"quic-transfer/internal/filemanager"
	"quic-transfer/internal/protocol"
)

var authToken = os.Getenv("QUIC_TOKEN")

//go:embed all:web
var webFiles embed.FS

const (
	storageDir = "./data/files"
	quicAddr   = ":4433"
	webAddr    = ":8443"
)

func main() {
	// Initialize shared file manager
	fm, err := filemanager.New(storageDir)
	if err != nil {
		log.Fatalf("[ERROR] Failed to initialize storage: %v", err)
	}

	log.Printf("[INFO] Storage directory: %s", storageDir)

	// Start QUIC binary protocol server in background
	go startQUICServer(fm)

	// Start HTTPS web server (blocking)
	startWebServer(fm)
}

// ══════════════════════════════════════════════════════
// QUIC Binary Protocol Server (for CLI)
// ══════════════════════════════════════════════════════

func startQUICServer(fm *filemanager.FileManager) {
	cert, err := tls.LoadX509KeyPair("certs/server.crt", "certs/server.key")
	if err != nil {
		log.Fatalf("[ERROR] Failed to load TLS certificate: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"quic-transfer"},
	}

	listener, err := quic.ListenAddr(quicAddr, tlsConfig, nil)
	if err != nil {
		log.Fatalf("[ERROR] Failed to start QUIC listener: %v", err)
	}
	defer listener.Close()

	log.Printf("[INFO] QUIC server listening on UDP %s", quicAddr)

	for {
		conn, err := listener.Accept(context.Background())
		if err != nil {
			log.Printf("[ERROR] QUIC accept failed: %v", err)
			continue
		}
		go handleQUICConnection(conn, fm)
	}
}

func handleQUICConnection(conn *quic.Conn, fm *filemanager.FileManager) {
	log.Printf("[INFO] QUIC client connected: %s", conn.RemoteAddr())

	for {
		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			log.Printf("[INFO] QUIC client disconnected: %s", conn.RemoteAddr())
			return
		}
		go handleQUICStream(stream, fm)
	}
}

func handleQUICStream(stream *quic.Stream, fm *filemanager.FileManager) {
	defer stream.Close()

	header, err := protocol.ReadHeader(stream)
	if err != nil {
		log.Printf("[ERROR] Failed to read header: %v", err)
		protocol.WriteResponse(stream, protocol.StatusError, "invalid header")
		return
	}

	if authToken != "" && header.Token != authToken {
		log.Printf("[WARN] Unauthorized QUIC request from %s", stream.StreamID())
		protocol.WriteResponse(stream, protocol.StatusError, "unauthorized")
		return
	}

	switch header.Command {
	case protocol.CmdUpload:
		handleQUICUpload(stream, header, fm)
	case protocol.CmdDownload:
		handleQUICDownload(stream, header, fm)
	case protocol.CmdList:
		handleQUICList(stream, fm)
	case protocol.CmdDelete:
		handleQUICDelete(stream, header, fm)
	case protocol.CmdStatus:
		handleQUICStatus(stream, header, fm)
	default:
		protocol.WriteResponse(stream, protocol.StatusError, "unknown command")
	}
}

func handleQUICUpload(stream *quic.Stream, header protocol.Header, fm *filemanager.FileManager) {
	log.Printf("[INFO] Upload started: %s (size: %s, offset: %d)", header.Filename, formatSize(header.FileSize), header.Offset)

	reader := io.LimitReader(stream, int64(header.FileSize-header.Offset))
	written, err := fm.SaveFileOffset(header.Filename, reader, header.Offset)
	if err != nil {
		log.Printf("[ERROR] Upload failed: %s — %v", header.Filename, err)
		protocol.WriteResponse(stream, protocol.StatusError, err.Error())
		return
	}

	// Read the 32-byte hash sent by the client
	clientHash := make([]byte, 32)
	if _, err := io.ReadFull(stream, clientHash); err != nil {
		log.Printf("[WARN] Failed to read client hash: %v", err)
	}

	// Calculate hash of the complete file on disk
	hasher := sha256.New()
	f, err := fm.OpenFile(header.Filename)
	if err == nil {
		io.Copy(hasher, f)
		f.Close()
		serverHash := hasher.Sum(nil)
		if string(serverHash) != string(clientHash) {
			msg := fmt.Sprintf("HASH MISMATCH! Upload corrupted. (Server: %x, Client: %x)", serverHash, clientHash)
			log.Printf("[ERROR] %s", msg)
			protocol.WriteResponse(stream, protocol.StatusError, msg)
			// Delete the corrupted file
			fm.DeleteFile(header.Filename)
			return
		}
	}

	msg := fmt.Sprintf("OK: %s (%s) [Hash Verified]", header.Filename, formatSize(uint64(written)+header.Offset))
	log.Printf("[INFO] Upload completed: %s (%s)", header.Filename, formatSize(uint64(written)+header.Offset))
	protocol.WriteResponse(stream, protocol.StatusOK, msg)
}

func handleQUICDownload(stream *quic.Stream, header protocol.Header, fm *filemanager.FileManager) {
	log.Printf("[INFO] Download requested: %s", header.Filename)

	f, err := fm.OpenFile(header.Filename)
	if err != nil {
		protocol.WriteResponse(stream, protocol.StatusError, err.Error())
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		protocol.WriteResponse(stream, protocol.StatusError, err.Error())
		return
	}

	protocol.WriteResponse(stream, protocol.StatusOK, fmt.Sprintf("%d", info.Size()))
	stream.Write([]byte("\n"))

	hasher := sha256.New()
	reader := io.TeeReader(f, hasher)

	written, err := io.Copy(stream, reader)
	if err != nil {
		log.Printf("[ERROR] Download transfer failed: %s — %v", header.Filename, err)
		return
	}
	
	// Send 32-byte hash
	serverHash := hasher.Sum(nil)
	stream.Write(serverHash)
	
	log.Printf("[INFO] Download completed: %s (%s)", header.Filename, formatSize(uint64(written)))
}

func handleQUICList(stream *quic.Stream, fm *filemanager.FileManager) {
	log.Printf("[INFO] File list requested (QUIC)")
	files, err := fm.ListFiles()
	if err != nil {
		protocol.WriteResponse(stream, protocol.StatusError, err.Error())
		return
	}

	result := ""
	for _, f := range files {
		result += fmt.Sprintf("%s\t%d\n", f.Name, f.Size)
	}
	if len(files) == 0 {
		result = "(no files)\n"
	}
	protocol.WriteResponse(stream, protocol.StatusOK, result)
}

func handleQUICDelete(stream *quic.Stream, header protocol.Header, fm *filemanager.FileManager) {
	log.Printf("[INFO] Delete requested: %s", header.Filename)
	if err := fm.DeleteFile(header.Filename); err != nil {
		log.Printf("[ERROR] Delete failed: %s — %v", header.Filename, err)
		protocol.WriteResponse(stream, protocol.StatusError, err.Error())
		return
	}
	log.Printf("[INFO] Deleted: %s", header.Filename)
	protocol.WriteResponse(stream, protocol.StatusOK, fmt.Sprintf("Deleted: %s", header.Filename))
}

func handleQUICStatus(stream *quic.Stream, header protocol.Header, fm *filemanager.FileManager) {
	log.Printf("[INFO] Status requested: %s", header.Filename)
	size, err := fm.FileSize(header.Filename)
	if err != nil {
		protocol.WriteResponse(stream, protocol.StatusError, err.Error())
		return
	}
	protocol.WriteResponse(stream, protocol.StatusOK, fmt.Sprintf("%d", size))
}

// ══════════════════════════════════════════════════════
// HTTPS Web Server (for browser)
// ══════════════════════════════════════════════════════

func startWebServer(fm *filemanager.FileManager) {
	mux := http.NewServeMux()

	// Serve static web files from embedded filesystem
	mux.HandleFunc("GET /", serveIndex)
	mux.HandleFunc("GET /static/", serveStatic)

	// REST API with optional auth middleware
	mux.Handle("GET /api/files", apiAuth(apiListFiles(fm)))
	mux.Handle("POST /api/upload", apiAuth(apiUploadFile(fm)))
	mux.Handle("GET /api/download/{filename}", apiAuth(apiDownloadFile(fm)))
	mux.Handle("DELETE /api/files/{filename}", apiAuth(apiDeleteFile(fm)))

	log.Printf("[INFO] Web server listening on https://localhost%s", webAddr)

	err := http.ListenAndServeTLS(webAddr, "certs/server.crt", "certs/server.key", mux)
	if err != nil {
		log.Fatalf("[ERROR] Web server failed: %v", err)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	// Map /static/app.js → web/app.js
	name := strings.TrimPrefix(r.URL.Path, "/static/")
	path := "web/" + name

	data, err := webFiles.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ext := filepath.Ext(name)
	switch ext {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Write(data)
}

// ── REST API Handlers ──────────────────────────────

func apiAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if authToken != "" {
			// Check Authorization header or token query param
			authHeader := r.Header.Get("Authorization")
			queryToken := r.URL.Query().Get("token")
			
			token := ""
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			} else if queryToken != "" {
				token = queryToken
			}

			if token != authToken {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	}
}

type fileJSON struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func apiListFiles(fm *filemanager.FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		files, err := fm.ListFiles()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		result := make([]fileJSON, len(files))
		for i, f := range files {
			result[i] = fileJSON{Name: f.Name, Size: f.Size}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func apiUploadFile(fm *filemanager.FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Limit upload size to 10 GB
		r.Body = http.MaxBytesReader(w, r.Body, 10<<30)

		file, handler, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Failed to read file: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		log.Printf("[INFO] Web upload started: %s", handler.Filename)

		written, err := fm.SaveFile(handler.Filename, file)
		if err != nil {
			log.Printf("[ERROR] Web upload failed: %s — %v", handler.Filename, err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[INFO] Web upload completed: %s (%s)", handler.Filename, formatSize(uint64(written)))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"filename": handler.Filename,
			"size":     written,
		})
	}
}

func apiDownloadFile(fm *filemanager.FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filename := r.PathValue("filename")
		if filename == "" {
			http.Error(w, "filename required", http.StatusBadRequest)
			return
		}

		f, err := fm.OpenFile(filename)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("[INFO] Web download: %s (%s)", filename, formatSize(uint64(info.Size())))

		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(filename)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
		io.Copy(w, f)
	}
}

func apiDeleteFile(fm *filemanager.FileManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filename := r.PathValue("filename")
		if filename == "" {
			http.Error(w, "filename required", http.StatusBadRequest)
			return
		}

		if err := fm.DeleteFile(filename); err != nil {
			log.Printf("[ERROR] Web delete failed: %s — %v", filename, err)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		log.Printf("[INFO] Web deleted: %s", filename)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"message": "Deleted: " + filename,
		})
	}
}

// ── Helpers ────────────────────────────────────────

func formatSize(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
