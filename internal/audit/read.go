package audit

import (
	"bytes"
	"encoding/json"
	"errors"
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
}

type Page struct {
	Items         []Event `json:"items"`
	Limit         int     `json:"limit"`
	Offset        int     `json:"offset"`
	HasMore       bool    `json:"has_more"`
	ScanTruncated bool    `json:"scan_truncated"`
	ParseErrors   int     `json:"parse_errors"`
}

const maxScanBytes int64 = 8 << 20

var archiveSuffix = regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}Z\.\d{6}$`)

func (r *Recorder) Query(q Query) (Page, error) {
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || q.Offset > 1000 {
		return Page{}, errors.New("invalid audit pagination")
	}
	w := r.writer
	w.mu.Lock()
	defer w.mu.Unlock()
	page := Page{Items: []Event{}, Limit: q.Limit, Offset: q.Offset}
	entries, err := os.ReadDir(filepath.Dir(w.path))
	if err != nil {
		return Page{}, err
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
	remaining := maxScanBytes
	matched := 0
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Page{}, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return Page{}, errors.New("unsafe audit file")
		}
		if remaining == 0 {
			page.ScanTruncated = true
			break
		}
		readSize := info.Size()
		partial := false
		if readSize > remaining {
			readSize = remaining
			partial = true
		}
		if readSize == 0 {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return Page{}, err
		}
		data := make([]byte, readSize)
		_, readErr := f.ReadAt(data, info.Size()-readSize)
		closeErr := f.Close()
		if readErr != nil && readErr != io.EOF {
			return Page{}, readErr
		}
		if closeErr != nil {
			return Page{}, closeErr
		}
		remaining -= readSize
		lines := bytes.Split(data, []byte{'\n'})
		start := 0
		if partial {
			start = 1
			page.ScanTruncated = true
		}
		for i := len(lines) - 1; i >= start; i-- {
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
				return page, nil
			}
			page.Items = append(page.Items, event)
		}
	}
	return page, nil
}
