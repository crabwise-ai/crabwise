package logwatcher

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/fsnotify/fsnotify"
)

// OffsetStore persists file read offsets for resume.
type OffsetStore interface {
	GetFileOffset(path string) (int64, error)
	SetFileOffset(path string, offset int64) error
}

type LogWatcher struct {
	logPaths     []string
	pollInterval time.Duration
	offsets      OffsetStore
	watcher      *fsnotify.Watcher
	polling      bool
	mu           sync.Mutex
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func New(logPaths []string, pollInterval time.Duration, offsets OffsetStore) *LogWatcher {
	return &LogWatcher{
		logPaths:     logPaths,
		pollInterval: pollInterval,
		offsets:      offsets,
	}
}

func (w *LogWatcher) Start(ctx context.Context, events chan<- *audit.AuditEvent) error {
	ctx, w.cancel = context.WithCancel(ctx)

	// Try fsnotify first
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("logwatcher: fsnotify unavailable, using polling: %v", err)
		w.polling = true
	} else {
		w.watcher = watcher
	}

	// Initial scan
	for _, logPath := range w.logPaths {
		w.scanAndWatch(logPath, events)
	}

	if w.polling {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.pollLoop(ctx, events)
		}()
	} else {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.watchLoop(ctx, events)
		}()
	}

	return nil
}

func (w *LogWatcher) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	if w.watcher != nil {
		return w.watcher.Close()
	}
	return nil
}

func (w *LogWatcher) CanEnforce() bool {
	return false // read-only adapter
}

func (w *LogWatcher) scanAndWatch(dir string, events chan<- *audit.AuditEvent) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			if w.watcher != nil {
				if watchErr := w.watcher.Add(path); watchErr != nil {
					log.Printf("logwatcher: watch error on %s: %v", path, watchErr)
					// If we can't add watches, fall back to polling
					w.mu.Lock()
					if !w.polling {
						w.polling = true
						log.Printf("logwatcher: falling back to polling")
					}
					w.mu.Unlock()
				}
			}
			return nil
		}

		if strings.HasSuffix(path, ".jsonl") {
			w.tailFile(path, events)
		}
		return nil
	})
}

func (w *LogWatcher) watchLoop(ctx context.Context, events chan<- *audit.AuditEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			if event.Op&fsnotify.Create != 0 {
				info, err := os.Stat(event.Name)
				if err != nil {
					continue
				}
				if info.IsDir() {
					w.watcher.Add(event.Name)
					continue
				}
				if strings.HasSuffix(event.Name, ".jsonl") {
					w.tailFile(event.Name, events)
				}
			}

			if event.Op&fsnotify.Write != 0 {
				if strings.HasSuffix(event.Name, ".jsonl") {
					w.tailFile(event.Name, events)
				}
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("logwatcher: fsnotify error: %v", err)
		}
	}
}

func (w *LogWatcher) pollLoop(ctx context.Context, events chan<- *audit.AuditEvent) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, logPath := range w.logPaths {
				w.scanAndWatch(logPath, events)
			}
		}
	}
}

func (w *LogWatcher) tailFile(path string, events chan<- *audit.AuditEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()

	offset, err := w.offsets.GetFileOffset(path)
	if err != nil {
		log.Printf("logwatcher: get offset for %s: %v", path, err)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	// Truncation detection
	info, err := f.Stat()
	if err != nil {
		return
	}
	if offset > info.Size() {
		offset = 0 // file was truncated, reset
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB max line

	var newOffset int64 = offset
	for scanner.Scan() {
		line := scanner.Bytes()
		newOffset += int64(len(line)) + 1 // +1 for newline

		parsed, err := ParseLine(line, path)
		if err != nil {
			continue
		}

		for _, evt := range parsed {
			// Tag with source file/offset for atomic commit with events
			evt.SourceFile = path
			evt.SourceOffset = newOffset

			select {
			case events <- evt:
			default:
				// Queue full, drop
			}
		}
	}
}
