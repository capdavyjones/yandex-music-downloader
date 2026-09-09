package batch

import (
	"context"
	"sync"
)

// PublishedFiles хранит путь первого успешно опубликованного файла по ID трека.
type PublishedFiles struct {
	mu      sync.Mutex
	entries map[string]*PublishedFile
}

// PublishedFile — ожидание первого воркера для конкретного ID.
type PublishedFile struct {
	once sync.Once
	done chan struct{}
	path string
}

func NewPublishedFiles() *PublishedFiles {
	return &PublishedFiles{entries: make(map[string]*PublishedFile)}
}

// Claim возвращает запись для ID. leader=true у первого воркера, который должен скачать файл.
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

// Complete публикует путь (пустая строка — скачивание не удалось) и будит ожидающих.
func (f *PublishedFile) Complete(path string) {
	f.once.Do(func() {
		f.path = path
		close(f.done)
	})
}

// Wait ждёт завершения первого воркера и возвращает путь к файлу.
func (f *PublishedFile) Wait(ctx context.Context) string {
	select {
	case <-f.done:
		return f.path
	case <-ctx.Done():
		return ""
	}
}
