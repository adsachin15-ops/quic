package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/quic-go/quic-go"
	"quic-transfer/internal/protocol"
)

func main() {
	serverAddr := flag.String("server", "localhost:4433", "Address of the QUIC relay server")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: client [-server <host:port>] <command> [args]")
		fmt.Println("Commands:")
		fmt.Println("  upload <filename>   - Host a file for direct relay")
		fmt.Println("  download <filename> - Join a relay and download a file")
		os.Exit(1)
	}

	command := args[0]

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // For self-signed certs
		NextProtos:         []string{"quic-transfer"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // Only for connection phase
	defer cancel()

	conn, err := quic.DialAddr(ctx, *serverAddr, tlsConfig, &quic.Config{
		MaxIdleTimeout: 0, // Keep connection alive indefinitely while waiting
		KeepAlivePeriod: 0,
	})
	if err != nil {
		log.Fatalf("Failed to connect to localhost:4433: %v", err)
	}
	defer conn.CloseWithError(0, "client exit")

	token := os.Getenv("QUIC_TOKEN")

	switch command {
	case "upload":
		if len(args) < 2 {
			log.Fatal("Usage: client upload <filename>")
		}
		uploadFile(conn, args[1], token)
	case "download":
		if len(args) < 2 {
			log.Fatal("Usage: client download <filename>")
		}
		downloadFile(conn, args[1], token)
	default:
		log.Fatalf("Unknown command: %s", command)
	}
}

func uploadFile(conn *quic.Conn, filename string, token string) {
	f, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Cannot open file: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		log.Fatalf("Cannot stat file: %v", err)
	}
	fileSize := uint64(info.Size())

	// We use a background context so the stream doesn't timeout while waiting for downloader
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("Failed to open stream: %v", err)
	}
	defer stream.Close()

	baseName := info.Name()
	header := protocol.Header{
		Command:  protocol.CmdUpload,
		Token:    token,
		Filename: baseName,
		FileSize: fileSize,
		Offset:   0,
	}

	if err := protocol.WriteHeader(stream, header); err != nil {
		log.Fatalf("Failed to send header: %v", err)
	}

	// Read initial response (Waiting for downloader...)
	status, msg, err := protocol.ReadResponse(stream)
	if err != nil {
		log.Fatalf("Server error: %v", err)
	}
	if status != protocol.StatusOK {
		log.Fatalf("Server rejected upload: %s", msg)
	}

	fmt.Printf("Hosting: %s (%s)\n", baseName, formatSize(fileSize))
	fmt.Printf("Waiting for a downloader to join...\n")

	hasher := sha256.New()
	reader := io.TeeReader(f, hasher)

	start := time.Now()
	
	// This will block until the downloader connects and reads data
	written, err := io.Copy(stream, reader)
	if err != nil {
		log.Fatalf("\nTransfer failed: %v", err)
	}
	
	hash := hasher.Sum(nil)
	stream.Write(hash) // Send hash

	// Wait for final confirmation
	finalStatus, finalMsg, _ := protocol.ReadResponse(stream)

	duration := time.Since(start).Seconds()
	speed := float64(written) / duration
	
	hashStatus := "Hash OK"
	if finalStatus != protocol.StatusOK {
		hashStatus = finalMsg
	}

	fmt.Printf("\n✓ OK: %s (%s) [%s] (%.1fs, %s/s)\n", baseName, formatSize(uint64(written)), hashStatus, duration, formatSize(uint64(speed)))
}

func downloadFile(conn *quic.Conn, filename string, token string) {
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("Failed to open stream: %v", err)
	}
	defer stream.Close()

	header := protocol.Header{
		Command:  protocol.CmdDownload,
		Token:    token,
		Filename: filename,
	}

	if err := protocol.WriteHeader(stream, header); err != nil {
		log.Fatalf("Failed to send header: %v", err)
	}

	// Read file size
	status, msg, err := protocol.ReadResponse(stream)
	if err != nil {
		log.Fatalf("Server error: %v", err)
	}
	if status != protocol.StatusOK {
		log.Fatalf("Download failed: %s", msg)
	}

	fileSizeStr := strings.TrimSpace(msg)
	fileSize, err := strconv.ParseUint(fileSizeStr, 10, 64)
	if err != nil {
		log.Fatalf("Invalid file size from server: %s", fileSizeStr)
	}

	fmt.Printf("Joining Relay: %s (%s)\n", filename, formatSize(fileSize))

	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("Failed to create local file: %v", err)
	}
	defer f.Close()

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	start := time.Now()
	
	// Read exactly fileSize bytes
	written, err := io.CopyN(writer, stream, int64(fileSize))
	if err != nil && err != io.EOF {
		log.Fatalf("\nTransfer failed: %v", err)
	}

	duration := time.Since(start).Seconds()
	if duration <= 0 {
		duration = 0.001
	}
	speed := float64(written) / duration

	fmt.Printf("\n✓ Downloaded: %s (%s, %.1fs, %s/s)\n", filename, formatSize(uint64(written)), duration, formatSize(uint64(speed)))
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
