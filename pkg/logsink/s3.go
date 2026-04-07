package logsink

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Sink stores logs as JSONL objects in S3.
// Each step gets a single object: s3://<bucket>/<prefix>/<org>/<run>/<step>.jsonl
//
// For running steps, the agent appends lines by re-uploading the object (or using
// multipart upload). For completed steps, the object is immutable.
//
// Tailing works by polling GetObject with a Range header to read only new bytes
// since the last read.
type S3Sink struct {
	Client *s3.Client
	Bucket string
	Prefix string // e.g., "logs/" — includes trailing slash
}

func (s *S3Sink) key(ref LogRef) string {
	return s.Prefix + ref.Path() + ".jsonl"
}

// Write appends log lines to the S3 object.
// For simplicity, this reads the existing object, appends, and re-uploads.
// For high-throughput production use, consider multipart upload or buffered writes.
func (s *S3Sink) Write(ctx context.Context, ref LogRef, lines []LogLine) error {
	key := s.key(ref)

	// Read existing content (if any).
	var existing []byte
	resp, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.Bucket,
		Key:    &key,
	})
	if err != nil {
		// If the object doesn't exist, start fresh.
		if !isS3NotFound(err) {
			return fmt.Errorf("logsink/s3: failed to read existing log: %w", err)
		}
	} else {
		existing, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	// Append new lines as JSONL.
	var buf bytes.Buffer
	buf.Write(existing)
	enc := json.NewEncoder(&buf)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			return fmt.Errorf("logsink/s3: failed to encode log line: %w", err)
		}
	}

	// Upload.
	_, err = s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.Bucket,
		Key:         &key,
		Body:        bytes.NewReader(buf.Bytes()),
		ContentType: aws.String("application/x-ndjson"),
	})
	if err != nil {
		return fmt.Errorf("logsink/s3: failed to upload log: %w", err)
	}

	return nil
}

// Read retrieves all log lines from the S3 object.
func (s *S3Sink) Read(ctx context.Context, ref LogRef) ([]LogLine, error) {
	key := s.key(ref)

	resp, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.Bucket,
		Key:    &key,
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("logsink/s3: failed to read log: %w", err)
	}
	defer resp.Body.Close()

	var lines []LogLine
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var line LogLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		lines = append(lines, line)
	}

	return lines, scanner.Err()
}

// Tail is implemented for the LogSink interface but should NOT be used for
// production real-time streaming. Real-time log streaming goes through the
// in-memory SSE path (agent → server → browser). This method exists only
// as a recovery fallback — e.g., if the server restarts mid-run and needs
// to catch up from S3.
//
// See docs/design/log-architecture.md for the full streaming architecture.
func (s *S3Sink) Tail(ctx context.Context, ref LogRef) (<-chan LogLine, error) {
	// For recovery: read all lines from S3, then close.
	// No polling — the SSE path handles real-time.
	ch := make(chan LogLine, 100)

	go func() {
		defer close(ch)
		lines, err := s.Read(ctx, ref)
		if err != nil {
			return
		}
		for _, line := range lines {
			select {
			case ch <- line:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// isS3NotFound checks if the error is a "not found" from S3.
func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	// Check for NoSuchKey error.
	var nsk *s3types.NoSuchKey
	if ok := errors.As(err, &nsk); ok {
		return true
	}
	// Fallback: check error message.
	return strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "404")
}
