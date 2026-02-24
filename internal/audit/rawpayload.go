package audit

import (
	"compress/zstd"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RawPayloadManager struct {
	dir       string
	maxSize   int64
	quota     int64
	retention time.Duration
}

func NewRawPayloadManager(dir string, maxSize, quota int64, retentionDays int) *RawPayloadManager {
	return &RawPayloadManager{
		dir:       dir,
		maxSize:   maxSize,
		quota:     quota,
		retention: time.Duration(retentionDays) * 24 * time.Hour,
	}
}

func (m *RawPayloadManager) Write(eventID string, payload []byte) (string, error) {
	if m == nil || strings.TrimSpace(eventID) == "" || len(payload) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return "", err
	}

	if int64(len(payload)) > m.maxSize {
		payload = payload[:m.maxSize]
	}

	ref := sanitizeEventRef(eventID)
	outPath := filepath.Join(m.dir, ref+".zst")
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()

	zw, err := zstd.NewWriter(f)
	if err != nil {
		return "", err
	}
	if _, err := zw.Write(payload); err != nil {
		_ = zw.Close()
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}

	return ref, nil
}

func (m *RawPayloadManager) Read(ref string) ([]byte, error) {
	if m == nil || strings.TrimSpace(ref) == "" {
		return nil, fmt.Errorf("raw payload ref required")
	}
	f, err := os.Open(filepath.Join(m.dir, sanitizeEventRef(ref)+".zst"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	zr, err := zstd.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	return io.ReadAll(zr)
}

func (m *RawPayloadManager) GC(now time.Time) error {
	if m == nil {
		return nil
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	type item struct {
		path    string
		size    int64
		modTime time.Time
	}
	var items []item
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zst") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(m.dir, e.Name())
		it := item{path: p, size: info.Size(), modTime: info.ModTime()}
		items = append(items, it)
		total += it.size
	}

	cutoff := now.Add(-m.retention)
	for _, it := range items {
		if it.modTime.Before(cutoff) {
			_ = os.Remove(it.path)
			total -= it.size
		}
	}

	if total <= m.quota {
		return nil
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.Before(items[j].modTime)
	})
	for _, it := range items {
		if total <= m.quota {
			break
		}
		if err := os.Remove(it.path); err == nil {
			total -= it.size
		}
	}
	return nil
}

func sanitizeEventRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.ReplaceAll(ref, "/", "_")
	ref = strings.ReplaceAll(ref, string(filepath.Separator), "_")
	return ref
}
