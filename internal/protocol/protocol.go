// Package protocol defines the wire format for the quic-transfer application.
//
// Wire format (v2):
//
//	┌─────────┬──────────┬───────┬──────────┬──────────┬──────────┬──────────┬───────────┐
//	│ Command │ TokenLen │ Token │ NameLen  │ Filename │ FileSize │  Offset  │ File Data │
//	│ 1 byte  │ 2B BE   │ N B   │ 2B BE   │  N B     │ 8B BE   │ 8B BE   │ varies    │
//	└─────────┴──────────┴───────┴──────────┴──────────┴──────────┴──────────┴───────────┘
//
// Commands:
//   - UPLOAD (1):   full header + file data → server stores file
//   - DOWNLOAD (2): full header (no data)   → server sends file back
//   - LIST (3):     command + token only    → server sends file list
//   - DELETE (4):   full header (no data)   → server deletes file
//   - STATUS (5):   full header (no data)   → server returns file size (for resume)
//
// Response:
//
//	┌────────┬──────────┐
//	│ Status │ Message  │
//	│ 1 byte │ rest     │
//	└────────┴──────────┘
package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	CmdUpload   byte = 1
	CmdDownload byte = 2
	CmdList     byte = 3
	CmdDelete   byte = 4
	CmdStatus   byte = 5 // Query server for current file size (resume support)

	StatusOK    byte = 0
	StatusError byte = 1

	MaxFilenameLen = 255
	MaxTokenLen    = 256
)

// Header is the metadata sent at the start of each stream.
type Header struct {
	Command  byte
	Token    string // Auth token (empty = no auth)
	Filename string
	FileSize uint64 // Total file size (for UPLOAD)
	Offset   uint64 // Resume offset (0 = start from beginning)
}

// WriteHeader writes the protocol header to w.
func WriteHeader(w io.Writer, h Header) error {
	// Command
	if _, err := w.Write([]byte{h.Command}); err != nil {
		return fmt.Errorf("write command: %w", err)
	}

	// Token (always present, can be zero-length)
	tokenBytes := []byte(h.Token)
	if len(tokenBytes) > MaxTokenLen {
		return fmt.Errorf("token too long: %d bytes", len(tokenBytes))
	}
	if err := binary.Write(w, binary.BigEndian, uint16(len(tokenBytes))); err != nil {
		return fmt.Errorf("write token length: %w", err)
	}
	if len(tokenBytes) > 0 {
		if _, err := w.Write(tokenBytes); err != nil {
			return fmt.Errorf("write token: %w", err)
		}
	}

	// LIST only needs command + token
	if h.Command == CmdList {
		return nil
	}

	// Filename
	nameBytes := []byte(h.Filename)
	if len(nameBytes) > MaxFilenameLen {
		return fmt.Errorf("filename too long: %d bytes", len(nameBytes))
	}
	if err := binary.Write(w, binary.BigEndian, uint16(len(nameBytes))); err != nil {
		return fmt.Errorf("write name length: %w", err)
	}
	if _, err := w.Write(nameBytes); err != nil {
		return fmt.Errorf("write filename: %w", err)
	}

	// FileSize + Offset
	if err := binary.Write(w, binary.BigEndian, h.FileSize); err != nil {
		return fmt.Errorf("write file size: %w", err)
	}
	if err := binary.Write(w, binary.BigEndian, h.Offset); err != nil {
		return fmt.Errorf("write offset: %w", err)
	}

	return nil
}

// ReadHeader reads the protocol header from r.
func ReadHeader(r io.Reader) (Header, error) {
	var h Header

	// Command
	cmdBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, cmdBuf); err != nil {
		return h, fmt.Errorf("read command: %w", err)
	}
	h.Command = cmdBuf[0]

	// Token
	var tokenLen uint16
	if err := binary.Read(r, binary.BigEndian, &tokenLen); err != nil {
		return h, fmt.Errorf("read token length: %w", err)
	}
	if tokenLen > MaxTokenLen {
		return h, fmt.Errorf("token too long: %d bytes", tokenLen)
	}
	if tokenLen > 0 {
		tokenBuf := make([]byte, tokenLen)
		if _, err := io.ReadFull(r, tokenBuf); err != nil {
			return h, fmt.Errorf("read token: %w", err)
		}
		h.Token = string(tokenBuf)
	}

	// LIST stops here
	if h.Command == CmdList {
		return h, nil
	}

	// Filename
	var nameLen uint16
	if err := binary.Read(r, binary.BigEndian, &nameLen); err != nil {
		return h, fmt.Errorf("read name length: %w", err)
	}
	if nameLen > MaxFilenameLen {
		return h, fmt.Errorf("filename too long: %d bytes", nameLen)
	}
	nameBuf := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBuf); err != nil {
		return h, fmt.Errorf("read filename: %w", err)
	}
	h.Filename = string(nameBuf)

	// FileSize + Offset
	if err := binary.Read(r, binary.BigEndian, &h.FileSize); err != nil {
		return h, fmt.Errorf("read file size: %w", err)
	}
	if err := binary.Read(r, binary.BigEndian, &h.Offset); err != nil {
		return h, fmt.Errorf("read offset: %w", err)
	}

	return h, nil
}

// WriteResponse writes a status + message to w.
func WriteResponse(w io.Writer, status byte, msg string) error {
	if _, err := w.Write([]byte{status}); err != nil {
		return err
	}
	_, err := w.Write([]byte(msg))
	return err
}

// ReadResponse reads a status + message from r.
func ReadResponse(r io.Reader) (byte, string, error) {
	statusBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, statusBuf); err != nil {
		return 0, "", fmt.Errorf("read status: %w", err)
	}
	msg, err := io.ReadAll(r)
	if err != nil {
		return 0, "", fmt.Errorf("read message: %w", err)
	}
	return statusBuf[0], string(msg), nil
}
