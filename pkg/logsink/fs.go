package logsink

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FilesystemSink stores logs as JSONL files on the local filesystem.
// Suitable for development and testing. Not HA.
type FilesystemSink struct {
	// BaseDir is the root directory for log storage.
	BaseDir string
}

func (f *FilesystemSink) logPath(ref LogRef) string {
	return filepath.Join(f.BaseDir, ref.Path()+".jsonl")
}

// Write appends log lines to a JSONL file.
func (f *FilesystemSink) Write(_ context.Context, ref LogRef, lines []LogLine) error {
	path := f.logPath(ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("logsink: failed to create directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("logsink: failed to open log file: %w", err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			return fmt.Errorf("logsink: failed to write log line: %w", err)
		}
	}

	return nil
}

// Read returns all log lines from the JSONL file.
func (f *FilesystemSink) Read(_ context.Context, ref LogRef) ([]LogLine, error) {
	path := f.logPath(ref)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("logsink: failed to open log file: %w", err)
	}
	defer file.Close()

	var lines []LogLine
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line LogLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue // skip malformed lines
		}
		lines = append(lines, line)
	}

	return lines, scanner.Err()
}

// Tail polls the log file for new lines. This is a simple polling implementation
// suitable for development — production uses S3Sink with different tailing logic.
func (f *FilesystemSink) Tail(ctx context.Context, ref LogRef) (<-chan LogLine, error) {
	ch := make(chan LogLine, 100)

	go func() {
		defer close(ch)
		var offset int64

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				path := f.logPath(ref)
				file, err := os.Open(path)
				if err != nil {
					continue
				}

				if _, err := file.Seek(offset, 0); err != nil {
					file.Close()
					continue
				}

				scanner := bufio.NewScanner(file)
				for scanner.Scan() {
					var line LogLine
					if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
						continue
					}
					select {
					case ch <- line:
					case <-ctx.Done():
						file.Close()
						return
					}
				}

				pos, _ := file.Seek(0, 1)
				offset = pos
				file.Close()
			}
		}
	}()

	return ch, nil
}
