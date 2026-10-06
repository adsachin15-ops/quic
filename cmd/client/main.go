// Phase 2: QUIC file transfer CLI client
//
// Usage:
//   ./client upload <file>       Upload a file to the server
//   ./client download <file>     Download a file from the server
//   ./client list                List files on the server
//   ./client delete <file>       Delete a file on the server
//
// What happens during an upload:
//
//   1. Client opens QUIC connection (TLS 1.3 handshake, ~1 RTT)
//   2. Client opens a new stream on that connection
//   3. Client sends protocol header: [UPLOAD][filename][size]
//   4. Client streams file data in chunks
//   5. Client closes write side (STREAM FIN)
//   6. Server writes file to disk, sends response
//   7. Client reads response, stream done
//
// The progress bar shows bytes sent vs total file size.
// Under the hood, "bytes sent" means bytes written to the QUIC stream.
// quic-go buffers and packetizes these into UDP datagrams.

package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
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

const serverAddr = "localhost:4433"
var authToken = os.Getenv("QUIC_TOKEN")

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "upload":
		if len(os.Args) < 3 {
			fmt.Println("Usage: client upload <file>")
			os.Exit(1)
		}
		doUpload(os.Args[2])
	case "download":
		if len(os.Args) < 3 {
			fmt.Println("Usage: client download <file>")
			os.Exit(1)
		}
		doDownload(os.Args[2])
	case "list":
		doList()
	case "delete":
		if len(os.Args) < 3 {
			fmt.Println("Usage: client delete <file>")
			os.Exit(1)
		}
		doDelete(os.Args[2])
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  client upload <file>      Upload a file")
	fmt.Println("  client download <file>    Download a file")
	fmt.Println("  client list               List files")
	fmt.Println("  client delete <file>      Delete a file")
}

// connect establishes a QUIC connection to the server.
func connect() *quic.Conn {
	tlsConfig := &tls.Config{
		NextProtos:         []string{"quic-transfer"},
		InsecureSkipVerify: true, // Self-signed cert for development
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, serverAddr, tlsConfig, nil)
	if err != nil {
		log.Fatalf("Failed to connect to %s: %v", serverAddr, err)
	}

	return conn
}

func doUpload(filepath string) {
	// Open the local file
	f, err := os.Open(filepath)
	if err != nil {
		log.Fatalf("Cannot open file: %v", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		log.Fatalf("Cannot stat file: %v", err)
	}

	filename := info.Name()
	fileSize := uint64(info.Size())

	fmt.Printf("Uploading: %s (%s)\n", filename, formatSize(fileSize))

	conn := connect()
	defer conn.CloseWithError(0, "done")

	offset := getStatus(conn, filename)
	if offset > fileSize {
		offset = 0 // Something is wrong, start over
	}

	if offset > 0 {
		if offset == fileSize {
			fmt.Printf("File '%s' is already fully uploaded on the server.\n", filename)
			return
		}
		fmt.Printf("Resuming upload from offset %s\n", formatSize(offset))
		if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
			log.Fatalf("Failed to seek file: %v", err)
		}
	}

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("Failed to open stream: %v", err)
	}

	// Send protocol header
	header := protocol.Header{
		Command:  protocol.CmdUpload,
		Token:    authToken,
		Filename: filename,
		FileSize: fileSize,
		Offset:   offset,
	}
	if err := protocol.WriteHeader(stream, header); err != nil {
		log.Fatalf("Failed to send header: %v", err)
	}

	// Stream the file data with progress tracking
	start := time.Now()
	written := int64(offset)
	buf := make([]byte, 32*1024) // 32 KB chunks

	hasher := sha256.New()
	
	// If resuming, we need to hash the existing part first
	if offset > 0 {
		fmt.Printf("Hashing existing data...\n")
		f.Seek(0, io.SeekStart)
		io.CopyN(hasher, f, int64(offset))
	}

	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, writeErr := stream.Write(buf[:n]); writeErr != nil {
				log.Fatalf("Failed to send data: %v", writeErr)
			}
			hasher.Write(buf[:n])
			written += int64(n)
			printProgress(written, int64(fileSize), start)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			log.Fatalf("Failed to read file: %v", readErr)
		}
	}
	
	// Send the calculated hash (32 bytes)
	hashBytes := hasher.Sum(nil)
	if _, err := stream.Write(hashBytes); err != nil {
		log.Fatalf("Failed to send hash: %v", err)
	}

	// Close write side — tells server "all data sent"
	stream.Close()

	// Read server response
	status, msg, err := protocol.ReadResponse(stream)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	fmt.Println() // newline after progress bar
	if status == protocol.StatusOK {
		elapsed := time.Since(start)
		speed := float64(written-int64(offset)) / elapsed.Seconds()
		fmt.Printf("✓ %s (%.1fs, %s/s)\n", msg, elapsed.Seconds(), formatSize(uint64(speed)))
	} else {
		fmt.Printf("✗ Error: %s\n", msg)
		os.Exit(1)
	}
}

func doDownload(filename string) {
	fmt.Printf("Downloading: %s\n", filename)

	conn := connect()
	defer conn.CloseWithError(0, "done")

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("Failed to open stream: %v", err)
	}

	// Send download request
	header := protocol.Header{
		Command:  protocol.CmdDownload,
		Token:    authToken,
		Filename: filename,
		FileSize: 0,
	}
	if err := protocol.WriteHeader(stream, header); err != nil {
		log.Fatalf("Failed to send header: %v", err)
	}
	stream.Close() // Done sending

	// Read response status
	statusBuf := make([]byte, 1)
	if _, err := io.ReadFull(stream, statusBuf); err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	if statusBuf[0] == protocol.StatusError {
		msg, _ := io.ReadAll(stream)
		fmt.Printf("✗ Error: %s\n", string(msg))
		os.Exit(1)
	}

	// Read file size (text until newline)
	var sizeBuf []byte
	for {
		b := make([]byte, 1)
		if _, err := io.ReadFull(stream, b); err != nil {
			log.Fatalf("Failed to read size: %v", err)
		}
		if b[0] == '\n' {
			break
		}
		sizeBuf = append(sizeBuf, b[0])
	}

	fileSize, err := strconv.ParseInt(string(sizeBuf), 10, 64)
	if err != nil {
		log.Fatalf("Invalid file size: %v", err)
	}

	fmt.Printf("Size: %s\n", formatSize(uint64(fileSize)))

	// Create local file
	outFile, err := os.Create(filename)
	if err != nil {
		log.Fatalf("Cannot create file: %v", err)
	}
	defer outFile.Close()

	// Download with progress
	start := time.Now()
	downloaded := int64(0)
	buf := make([]byte, 32*1024)
	
	hasher := sha256.New()

	for downloaded < fileSize {
		n, readErr := stream.Read(buf)
		if n > 0 {
			if _, writeErr := outFile.Write(buf[:n]); writeErr != nil {
				log.Fatalf("Failed to write file: %v", writeErr)
			}
			hasher.Write(buf[:n])
			downloaded += int64(n)
			printProgress(downloaded, fileSize, start)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			log.Fatalf("Download error: %v", readErr)
		}
	}

	// Read server's hash
	serverHash := make([]byte, 32)
	if _, err := io.ReadFull(stream, serverHash); err != nil && err != io.EOF {
		log.Fatalf("Failed to read server hash: %v", err)
	}
	localHash := hasher.Sum(nil)
	
	hashStatus := "Hash OK"
	if string(serverHash) != string(localHash) {
		hashStatus = fmt.Sprintf("HASH MISMATCH (Server: %x, Local: %x)", serverHash, localHash)
	}

	elapsed := time.Since(start)
	speed := float64(downloaded) / elapsed.Seconds()
	fmt.Printf("\n✓ Downloaded: %s (%s, %.1fs, %s/s) [%s]\n",
		filename, formatSize(uint64(downloaded)), elapsed.Seconds(), formatSize(uint64(speed)), hashStatus)
}

func doList() {
	conn := connect()
	defer conn.CloseWithError(0, "done")

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("Failed to open stream: %v", err)
	}

	// Send list request
	header := protocol.Header{
		Command: protocol.CmdList,
		Token:   authToken,
	}
	if err := protocol.WriteHeader(stream, header); err != nil {
		log.Fatalf("Failed to send header: %v", err)
	}
	stream.Close()

	// Read response
	status, msg, err := protocol.ReadResponse(stream)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	if status == protocol.StatusError {
		fmt.Printf("✗ Error: %s\n", msg)
		os.Exit(1)
	}

	// Parse and display file list
	fmt.Println("Files on server:")
	fmt.Println("─────────────────────────────────────────────")
	fmt.Printf("%-30s %10s\n", "NAME", "SIZE")
	fmt.Println("─────────────────────────────────────────────")

	lines := strings.Split(strings.TrimSpace(msg), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			size, _ := strconv.ParseUint(parts[1], 10, 64)
			fmt.Printf("%-30s %10s\n", parts[0], formatSize(size))
		} else {
			fmt.Println(line)
		}
	}
}

func doDelete(filename string) {
	// Ask for confirmation
	fmt.Printf("Delete '%s' from server? [y/N]: ", filename)
	var answer string
	fmt.Scanln(&answer)
	if answer != "y" && answer != "Y" {
		fmt.Println("Cancelled.")
		return
	}

	conn := connect()
	defer conn.CloseWithError(0, "done")

	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("Failed to open stream: %v", err)
	}

	header := protocol.Header{
		Command:  protocol.CmdDelete,
		Token:    authToken,
		Filename: filename,
		FileSize: 0,
	}
	if err := protocol.WriteHeader(stream, header); err != nil {
		log.Fatalf("Failed to send header: %v", err)
	}
	stream.Close()

	status, msg, err := protocol.ReadResponse(stream)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	if status == protocol.StatusOK {
		fmt.Printf("✓ %s\n", msg)
	} else {
		fmt.Printf("✗ Error: %s\n", msg)
		os.Exit(1)
	}
}

// getStatus queries the server for the current size of a file.
func getStatus(conn *quic.Conn, filename string) uint64 {
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		return 0
	}
	defer stream.Close()

	header := protocol.Header{
		Command:  protocol.CmdStatus,
		Token:    authToken,
		Filename: filename,
	}
	if err := protocol.WriteHeader(stream, header); err != nil {
		return 0
	}
	stream.Close() // Done sending

	status, msg, err := protocol.ReadResponse(stream)
	if err == nil && status == protocol.StatusOK {
		size, _ := strconv.ParseUint(msg, 10, 64)
		return size
	}
	return 0
}

// printProgress displays a progress bar with speed.
func printProgress(current, total int64, start time.Time) {
	if total <= 0 {
		return
	}

	elapsed := time.Since(start).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}

	pct := float64(current) / float64(total) * 100
	speed := float64(current) / elapsed

	barWidth := 30
	filled := int(pct / 100 * float64(barWidth))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	fmt.Printf("\r  %s %5.1f%%  %s / %s  %s/s",
		bar, pct,
		formatSize(uint64(current)),
		formatSize(uint64(total)),
		formatSize(uint64(speed)))
}

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
