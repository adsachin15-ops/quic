// Package filemanager handles safe file storage operations.
//
// All files are stored in a single directory (the storage root).
// This package enforces:
//   - No path traversal (../ or absolute paths)
//   - Filenames are sanitized to base name only
//   - All operations are confined to the storage root
package filemanager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FileManager manages files within a safe storage directory.
type FileManager struct {
	root string // Absolute path to storage directory
}

// FileInfo holds metadata about a stored file.
type FileInfo struct {
	Name string
	Size int64
}

// New creates a FileManager rooted at the given directory.
// Creates the directory if it doesn't exist.
func New(root string) (*FileManager, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve storage path: %w", err)
	}

	if err := os.MkdirAll(absRoot, 0755); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}

	return &FileManager{root: absRoot}, nil
}

// safePath resolves a filename to an absolute path within the storage root.
// Returns an error if the resolved path escapes the root directory.
//
// This is the critical security function — it prevents path traversal:
//   - "../../etc/passwd"  → error (escapes root)
//   - "/etc/passwd"       → error (absolute path)
//   - "subdir/file.txt"   → uses only "file.txt" (base name)
//   - "normal.txt"        → root/normal.txt (OK)
func (fm *FileManager) safePath(filename string) (string, error) {
	// Take only the base name — strips any directory components
	clean := filepath.Base(filename)

	// filepath.Base returns "." for empty strings
	if clean == "." || clean == ".." || clean == "" {
		return "", fmt.Errorf("invalid filename: %q", filename)
	}

	// Reject hidden files (starting with .)
	if strings.HasPrefix(clean, ".") {
		return "", fmt.Errorf("hidden files not allowed: %q", clean)
	}

	full := filepath.Join(fm.root, clean)

	// Final check: resolved path must be inside the root
	if !strings.HasPrefix(full, fm.root+string(filepath.Separator)) && full != fm.root {
		return "", fmt.Errorf("path traversal detected: %q", filename)
	}

	return full, nil
}

// SaveFile writes data from r to a file with the given name.
// Returns the number of bytes written.
func (fm *FileManager) SaveFile(filename string, r io.Reader) (int64, error) {
	return fm.SaveFileOffset(filename, r, 0)
}

// SaveFileOffset writes data to a file starting at the given offset.
// If offset is 0, it truncates the file. Otherwise, it appends to it,
// returning an error if the existing file size doesn't match the offset.
func (fm *FileManager) SaveFileOffset(filename string, r io.Reader, offset uint64) (int64, error) {
	path, err := fm.safePath(filename)
	if err != nil {
		return 0, err
	}

	var f *os.File
	if offset == 0 {
		f, err = os.Create(path)
	} else {
		// Check existing size
		info, err := os.Stat(path)
		if err != nil {
			return 0, fmt.Errorf("stat file for resume: %w", err)
		}
		if uint64(info.Size()) != offset {
			return 0, fmt.Errorf("resume offset mismatch: got %d, expected %d", info.Size(), offset)
		}

		f, err = os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	}

	if err != nil {
		return 0, fmt.Errorf("open file for writing: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(f, r)
	if err != nil {
		return 0, fmt.Errorf("write file: %w", err)
	}

	return written, nil
}

// OpenFile opens a stored file for reading.
// Caller must close the returned file.
func (fm *FileManager) OpenFile(filename string) (*os.File, error) {
	path, err := fm.safePath(filename)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	return f, nil
}

// DeleteFile removes a stored file.
func (fm *FileManager) DeleteFile(filename string) error {
	path, err := fm.safePath(filename)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete file: %w", err)
	}

	return nil
}

// ListFiles returns metadata for all files in the storage directory.
func (fm *FileManager) ListFiles() ([]FileInfo, error) {
	entries, err := os.ReadDir(fm.root)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var files []FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		// Skip hidden files
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name: entry.Name(),
			Size: info.Size(),
		})
	}

	return files, nil
}

// FileSize returns the size of a stored file.
func (fm *FileManager) FileSize(filename string) (int64, error) {
	path, err := fm.safePath(filename)
	if err != nil {
		return 0, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat file: %w", err)
	}

	return info.Size(), nil
}
