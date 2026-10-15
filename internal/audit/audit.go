package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"hydra-admin/internal/observability"
)

type Event struct {
	Timestamp     time.Time `json:"timestamp"`
	RequestID     string    `json:"request_id,omitempty"`
	Actor         string    `json:"actor,omitempty"`
	SourceIP      string    `json:"source_ip,omitempty"`
	Action        string    `json:"action"`
	TargetType    string    `json:"target_type,omitempty"`
	TargetID      string    `json:"target_id,omitempty"`
	Outcome       string    `json:"outcome"`
	HydraStatus   int       `json:"hydra_status,omitempty"`
	ChangedFields []string  `json:"changed_fields,omitempty"`
}

type Recorder struct{ writer *RotatingWriter }

func NewRecorder(writer *RotatingWriter) *Recorder { return &Recorder{writer: writer} }

// Ready checks local durability before an operation that must be audited.
func (r *Recorder) Ready() error { return r.writer.Sync() }

func (r *Recorder) Append(event Event) error {
	if event.Action == "" || event.Outcome == "" {
		return errors.New("audit action and outcome are required")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	event.RequestID = observability.SanitizeString(event.RequestID)
	event.Actor = observability.SanitizeString(event.Actor)
	event.SourceIP = observability.SanitizeString(event.SourceIP)
	event.Action = observability.SanitizeString(event.Action)
	event.TargetType = observability.SanitizeString(event.TargetType)
	event.TargetID = observability.SanitizeString(event.TargetID)
	event.Outcome = observability.SanitizeString(event.Outcome)
	if event.ChangedFields != nil {
		fields := make([]string, len(event.ChangedFields))
		for i, field := range event.ChangedFields {
			fields[i] = observability.SanitizeString(field)
		}
		event.ChangedFields = fields
	}
	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode audit event: %w", err)
	}
	line = append(line, '\n')
	if _, err := r.writer.Write(line); err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	if err := r.writer.Sync(); err != nil {
		return fmt.Errorf("sync audit event: %w", err)
	}
	return nil
}

// AppLogWriter reports rotation failures to stderr and keeps an unhealthy flag for status reporting.
type AppLogWriter struct {
	writer    *RotatingWriter
	stderr    io.Writer
	unhealthy atomic.Bool
}

func NewAppLogWriter(writer *RotatingWriter, stderr io.Writer) *AppLogWriter {
	if stderr == nil {
		stderr = os.Stderr
	}
	return &AppLogWriter{writer: writer, stderr: stderr}
}
func (w *AppLogWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err != nil {
		w.unhealthy.Store(true)
		fmt.Fprintln(w.stderr, "application log write or rotation failed")
	}
	return n, err
}
func (w *AppLogWriter) Healthy() bool { return !w.unhealthy.Load() }
func (w *AppLogWriter) Close() error {
	err := w.writer.Close()
	if err != nil {
		w.unhealthy.Store(true)
		fmt.Fprintln(w.stderr, "application log close failed")
	}
	return err
}
