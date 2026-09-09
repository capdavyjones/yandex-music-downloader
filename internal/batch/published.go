package batch

import (
	"context"
	"sync"
)

// PublishedFiles stores the path of the first successfully published file per track ID.
type PublishedFiles struct {
	mu      sync.Mutex
	entries map[string]*PublishedFile
}

// PublishedFile lets workers wait for the first worker of a given ID.
type PublishedFile struct {
	once sync.Once
	done chan struct{}
	path string
}

func NewPublishedFiles() *PublishedFiles {
	return &PublishedFiles{entries: make(map[string]*PublishedFile)}
}

// Claim returns the entry for an ID. leader is true for the first worker, which must download the file.
func (p *PublishedFiles) Claim(id string) (*PublishedFile, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.entries[id]; ok {
		return entry, false
	}
	entry := &PublishedFile{done: make(chan struct{})}
	p.entries[id] = entry
	return entry, true
}

// Complete publishes the path (an empty string means the download failed) and wakes up waiters.
func (f *PublishedFile) Complete(path string) {
	f.once.Do(func() {
		f.path = path
		close(f.done)
	})
}

// Wait blocks until the first worker finishes and returns the file path.
func (f *PublishedFile) Wait(ctx context.Context) string {
	select {
	case <-f.done:
		return f.path
	case <-ctx.Done():
		return ""
	}
}
