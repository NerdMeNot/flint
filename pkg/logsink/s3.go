package logsink

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Sink stores logs as JSONL SEGMENT objects in S3:
//
//	s3://<bucket>/<prefix><org>/<run>/<step>/<seq>-<uniq>.jsonl
//
// Each Write is ONE PutObject of just that batch — never a read-modify-write
// of the whole log (which would be O(n²) bandwidth in log size). Reads list
// the step's segment objects (lexicographic key order = write order thanks to
// the zero-padded nanosecond sequence) and concatenate.
type S3Sink struct {
	Client *s3.Client
	Bucket string
	Prefix string // e.g., "logs/" — includes trailing slash
}

// NewS3Sink creates an S3Sink using the default AWS credential chain. An
// optional endpoint overrides the S3 API URL for S3-compatible stores
// (MinIO, R2, Ceph). Logs are stored under the "logs/" key prefix.
func NewS3Sink(ctx context.Context, bucket, region, endpoint string) (*S3Sink, error) {
	if bucket == "" {
		return nil, fmt.Errorf("logsink/s3: bucket must not be empty")
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("logsink/s3: load AWS config: %w", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true // S3-compatible stores generally require path-style
		}
	})
	return &S3Sink{Client: client, Bucket: bucket, Prefix: "logs/"}, nil
}

// segmentDir is the key prefix holding a step's log segments.
func (s *S3Sink) segmentDir(ref LogRef) string {
	return s.Prefix + ref.Path() + "/"
}

// Write uploads this batch as a NEW segment object — one PUT, no read-back.
func (s *S3Sink) Write(ctx context.Context, ref LogRef, lines []LogLine) error {
	if len(lines) == 0 {
		return nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			return fmt.Errorf("logsink/s3: failed to encode log line: %w", err)
		}
	}

	// Zero-padded nanosecond sequence orders segments lexicographically; the
	// random suffix keeps concurrent writers (server replicas) from colliding.
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	key := fmt.Sprintf("%s%020d-%s.jsonl", s.segmentDir(ref), time.Now().UnixNano(), hex.EncodeToString(suffix))

	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.Bucket,
		Key:         &key,
		Body:        bytes.NewReader(buf.Bytes()),
		ContentType: aws.String("application/x-ndjson"),
	})
	if err != nil {
		return fmt.Errorf("logsink/s3: failed to upload log segment: %w", err)
	}
	return nil
}

// Read lists the step's segments in key order and concatenates their lines.
func (s *S3Sink) Read(ctx context.Context, ref LogRef) ([]LogLine, error) {
	dir := s.segmentDir(ref)
	paginator := s3.NewListObjectsV2Paginator(s.Client, &s3.ListObjectsV2Input{
		Bucket: &s.Bucket,
		Prefix: aws.String(dir),
	})

	var keys []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("logsink/s3: list segments: %w", err)
		}
		for _, obj := range page.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
	}
	sort.Strings(keys)

	var lines []LogLine
	for _, key := range keys {
		resp, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: &s.Bucket,
			Key:    &key,
		})
		if err != nil {
			if isS3NotFound(err) {
				continue
			}
			return nil, fmt.Errorf("logsink/s3: read segment %s: %w", key, err)
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			var line LogLine
			if json.Unmarshal(scanner.Bytes(), &line) == nil {
				lines = append(lines, line)
			}
		}
		err = scanner.Err()
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("logsink/s3: scan segment %s: %w", key, err)
		}
	}
	return lines, nil
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
