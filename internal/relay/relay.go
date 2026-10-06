package relay

import (
	"errors"
	"io"
	"sync"
	"time"
)

var ErrTimeout = errors.New("timeout waiting for peer")

type Session struct {
	Filename string
	FileSize uint64
	Uploader io.Reader
	
	ready chan struct{}
	done  chan error
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

// Host creates a new relay session and waits for a downloader to connect.
// If a downloader connects, it blocks until the download completes.
func (m *Manager) Host(filename string, size uint64, uploader io.Reader, timeout time.Duration) error {
	m.mu.Lock()
	if _, exists := m.sessions[filename]; exists {
		m.mu.Unlock()
		return errors.New("file is already being hosted")
	}
	
	session := &Session{
		Filename: filename,
		FileSize: size,
		Uploader: uploader,
		ready:    make(chan struct{}),
		done:     make(chan error, 1),
	}
	m.sessions[filename] = session
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.sessions, filename)
		m.mu.Unlock()
	}()

	select {
	case <-session.ready:
		// Downloader connected, wait for finish
		return <-session.done
	case <-time.After(timeout):
		return ErrTimeout
	}
}

// Join connects to an existing session, returning the file size and a reader for the data.
// The caller must read from the returned reader and then call Finish.
func (m *Manager) Join(filename string) (uint64, io.Reader, func(error), error) {
	m.mu.Lock()
	session, exists := m.sessions[filename]
	if !exists {
		m.mu.Unlock()
		return 0, nil, nil, errors.New("file not found or sender not waiting")
	}
	
	// Remove it so no one else can join
	delete(m.sessions, filename)
	m.mu.Unlock()

	// Signal that we are ready
	close(session.ready)

	finishFunc := func(err error) {
		session.done <- err
	}
	
	return session.FileSize, session.Uploader, finishFunc, nil
}
