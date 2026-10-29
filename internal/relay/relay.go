package relay

import (
	"errors"
	"io"
	"sync"
	"time"
)

type Session struct {
	Uploader   io.Reader
	Downloader io.Writer
	FileSize   uint64
	Ready      chan struct{} // Closed when uploader is ready
	Joined     chan struct{} // Closed when downloader has joined
	Done       chan struct{} // Closed when transfer is done
	Err        error
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// Host creates a new session for a filename. It blocks until a downloader connects and the transfer finishes,
// or times out if no downloader connects within 15 minutes.
func (m *Manager) Host(filename string, filesize uint64, uploader io.Reader) error {
	m.mu.Lock()
	if _, exists := m.sessions[filename]; exists {
		m.mu.Unlock()
		return errors.New("a file with this name is already being hosted")
	}

	session := &Session{
		Uploader: uploader,
		FileSize: filesize,
		Ready:    make(chan struct{}),
		Joined:   make(chan struct{}),
		Done:     make(chan struct{}),
	}
	m.sessions[filename] = session
	m.mu.Unlock()

	// Signal that the uploader is ready
	close(session.Ready)

	// Wait for a downloader to join, with a 15-minute timeout
	select {
	case <-session.Joined:
		// Downloader joined! Now wait for the transfer to complete.
		<-session.Done
	case <-time.After(15 * time.Minute):
		m.mu.Lock()
		delete(m.sessions, filename)
		m.mu.Unlock()
		return errors.New("transfer timed out waiting for downloader")
	}

	m.mu.Lock()
	delete(m.sessions, filename)
	m.mu.Unlock()

	return session.Err
}

// Join connects to an existing session and copies data from the uploader to the downloader.
func (m *Manager) Join(filename string, downloader io.Writer) (uint64, error) {
	m.mu.Lock()
	session, exists := m.sessions[filename]
	if !exists {
		m.mu.Unlock()
		return 0, errors.New("no active upload found for this filename")
	}
	// We remove it immediately so no one else can join
	delete(m.sessions, filename)
	m.mu.Unlock()

	// Signal that the downloader has joined
	close(session.Joined)

	// Wait until the uploader is fully ready (should be immediate)
	<-session.Ready

	// Copy data directly from uploader to downloader in RAM
	written, err := io.Copy(downloader, session.Uploader)
	session.Err = err
	
	// Signal the uploader that we are done
	close(session.Done)

	return uint64(written), err
}

func (m *Manager) GetFileSize(filename string) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session, exists := m.sessions[filename]; exists {
		return session.FileSize, nil
	}
	return 0, errors.New("file not found")
}

type FileInfo struct {
	Name string `json:"name"`
	Size uint64 `json:"size"`
}

// List returns a list of currently hosted files waiting for download.
func (m *Manager) List() []FileInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]FileInfo, 0, len(m.sessions))
	for name, session := range m.sessions {
		list = append(list, FileInfo{Name: name, Size: session.FileSize})
	}
	return list
}
