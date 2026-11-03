package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Query is deliberately limited to the configured audit file and archives made
// by RotatingWriter. No caller can supply a path.
type Query struct {
	From, To         time.Time
	Action, ClientID string
	Limit, Offset    int
	Cursor           string
}

type Page struct {
	Items         []Event `json:"items"`
	Limit         int     `json:"limit"`
	Offset        int     `json:"offset"`
	HasMore       bool    `json:"has_more"`
	ScanTruncated bool    `json:"scan_truncated"`
	ParseErrors   int     `json:"parse_errors"`
	NextCursor    string  `json:"next_cursor,omitempty"`
}

const maxScanBytes int64 = 8 << 20

var ErrInvalidCursor = errors.New("invalid or expired audit cursor")

var archiveSuffix = regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}Z\.\d{6}$`)

type scanCursor struct {
	File        string `json:"file"`
	Offset      int64  `json:"offset"`
	Filter      string `json:"filter"`
	Fingerprint string `json:"fingerprint"`
}

func filterDigest(q Query) string {
	value := fmt.Sprintf("%s\x00%s\x00%s\x00%s", q.From.UTC().Format(time.RFC3339Nano), q.To.UTC().Format(time.RFC3339Nano), q.Action, q.ClientID)
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func encodeCursor(file string, offset int64, fingerprint string, q Query) string {
	data, _ := json.Marshal(scanCursor{File: file, Offset: offset, Filter: filterDigest(q), Fingerprint: fingerprint})
	return base64.RawURLEncoding.EncodeToString(data)
}

func fileFingerprint(f *os.File, size int64) (string, error) {
	length := min(size, 64)
	data := make([]byte, length)
	if length > 0 {
		if _, err := f.ReadAt(data, 0); err != nil {
			return "", err
		}
	}
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

type auditSnapshotFile struct {
	name string
	file *os.File
	size int64
}

// Open a bounded set of file handles while rotation is locked. The scan then
// reads those handles without blocking audit appends or readiness checks.
func (r *Recorder) snapshotFiles() ([]auditSnapshotFile, error) {
	w := r.writer
	w.mu.Lock()
	defer w.mu.Unlock()
	entries, err := os.ReadDir(filepath.Dir(w.path))
	if err != nil {
		return nil, err
	}
	prefix := filepath.Base(w.path) + "."
	archives := []string{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		if archiveSuffix.MatchString(strings.TrimPrefix(entry.Name(), prefix)) {
			archives = append(archives, filepath.Join(filepath.Dir(w.path), entry.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(archives)))
	if len(archives) > w.maxArchives {
		archives = archives[:w.maxArchives]
	}
	paths := append([]string{w.path}, archives...)
	files := make([]auditSnapshotFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			closeSnapshotFiles(files)
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			closeSnapshotFiles(files)
			return nil, errors.New("unsafe audit file")
		}
		f, err := os.Open(path)
		if err != nil {
			closeSnapshotFiles(files)
			return nil, err
		}
		opened, err := f.Stat()
		if err != nil || !os.SameFile(info, opened) {
			f.Close()
			closeSnapshotFiles(files)
			return nil, errors.New("audit file changed while opening")
		}
		files = append(files, auditSnapshotFile{name: filepath.Base(path), file: f, size: opened.Size()})
	}
	return files, nil
}

func closeSnapshotFiles(files []auditSnapshotFile) {
	for _, file := range files {
		file.file.Close()
	}
}

func (r *Recorder) Query(q Query) (Page, error) {
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 1000 || len(q.Cursor) > 4096 || (q.Cursor != "" && q.Offset != 0) {
		return Page{}, errors.New("invalid audit pagination")
	}
	page := Page{Items: []Event{}, Limit: q.Limit, Offset: q.Offset}
	files, err := r.snapshotFiles()
	if err != nil {
		return Page{}, err
	}
	defer closeSnapshotFiles(files)
	startIndex := 0
	startOffset := int64(-1)
	expectedFingerprint := ""
	if q.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil {
			return Page{}, ErrInvalidCursor
		}
		var cursor scanCursor
		if json.Unmarshal(data, &cursor) != nil || cursor.Offset < 0 || cursor.Filter != filterDigest(q) || cursor.Fingerprint == "" {
			return Page{}, ErrInvalidCursor
		}
		startIndex = -1
		for i, file := range files {
			if file.name == cursor.File {
				startIndex, startOffset, expectedFingerprint = i, cursor.Offset, cursor.Fingerprint
				break
			}
		}
		if startIndex < 0 {
			return Page{}, ErrInvalidCursor
		}
	}
	remaining := maxScanBytes
	matched := 0
	for i := startIndex; i < len(files); i++ {
		file := files[i]
		fingerprint, err := fileFingerprint(file.file, file.size)
		if err != nil {
			return Page{}, err
		}
		if i == startIndex && expectedFingerprint != "" && fingerprint != expectedFingerprint {
			return Page{}, ErrInvalidCursor
		}
		position := file.size
		if i == startIndex && startOffset >= 0 {
			if startOffset > position {
				return Page{}, ErrInvalidCursor
			}
			position = startOffset
		}
		if position == 0 {
			continue
		}
		if remaining == 0 {
			page.ScanTruncated = true
			page.NextCursor = encodeCursor(file.name, position, fingerprint, q)
			page.HasMore = true
			return page, nil
		}
		readSize := position
		if readSize > remaining {
			readSize = remaining
		}
		partial := readSize < position
		data := make([]byte, readSize)
		dataStart := position - readSize
		_, readErr := file.file.ReadAt(data, dataStart)
		if readErr != nil && readErr != io.EOF {
			return Page{}, readErr
		}
		remaining -= readSize
		lines := bytes.Split(data, []byte{'\n'})
		start := 0
		if partial {
			start = 1
		}
		lineEnd := position
		for i := len(lines) - 1; i >= start; i-- {
			segmentEnd := lineEnd
			lineStart := lineEnd - int64(len(lines[i]))
			lineEnd = lineStart - 1
			line := bytes.TrimSpace(lines[i])
			if len(line) == 0 {
				continue
			}
			var event Event
			if json.Unmarshal(line, &event) != nil || event.Timestamp.IsZero() || event.Action == "" || event.Outcome == "" {
				page.ParseErrors++
				continue
			}
			if !q.From.IsZero() && event.Timestamp.Before(q.From) {
				continue
			}
			if !q.To.IsZero() && event.Timestamp.After(q.To) {
				continue
			}
			if q.Action != "" && event.Action != q.Action {
				continue
			}
			if q.ClientID != "" && event.TargetID != q.ClientID {
				continue
			}
			matched++
			if matched <= q.Offset {
				continue
			}
			if len(page.Items) == q.Limit {
				page.HasMore = true
				page.NextCursor = encodeCursor(file.name, segmentEnd, fingerprint, q)
				return page, nil
			}
			page.Items = append(page.Items, event)
		}
		if partial {
			page.ScanTruncated = true
			// The skipped leading fragment is revisited with its complete line.
			resume := dataStart
			if len(lines) > 1 {
				resume += int64(len(lines[0])) + 1
			} else {
				page.ParseErrors++ // An individual record exceeds the scan window.
			}
			if resume > 0 {
				page.NextCursor = encodeCursor(file.name, resume, fingerprint, q)
				page.HasMore = true
				return page, nil
			}
		}
	}
	return page, nil
}
