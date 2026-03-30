package logsink_test

import (
	"context"
	"testing"
	"time"

	"github.com/NerdMeNot/flint/pkg/logsink"
)

func TestFilesystemSink_WriteAndRead(t *testing.T) {
	sink := &logsink.FilesystemSink{BaseDir: t.TempDir()}
	ctx := context.Background()

	ref := logsink.LogRef{
		OrgID:    "org1",
		RunID:    "run1",
		StepName: "test",
	}

	lines := []logsink.LogLine{
		{Timestamp: time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC), Stream: "stdout", Content: "running tests..."},
		{Timestamp: time.Date(2026, 3, 28, 12, 0, 1, 0, time.UTC), Stream: "stdout", Content: "PASS"},
		{Timestamp: time.Date(2026, 3, 28, 12, 0, 1, 0, time.UTC), Stream: "stderr", Content: "warning: deprecated"},
	}

	// Write.
	if err := sink.Write(ctx, ref, lines); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	// Read back.
	got, err := sink.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("len(Read()) = %d, want 3", len(got))
	}
	if got[0].Content != "running tests..." {
		t.Errorf("line[0].Content = %q", got[0].Content)
	}
	if got[1].Content != "PASS" {
		t.Errorf("line[1].Content = %q", got[1].Content)
	}
	if got[2].Stream != "stderr" {
		t.Errorf("line[2].Stream = %q, want stderr", got[2].Stream)
	}
}

func TestFilesystemSink_AppendWrite(t *testing.T) {
	sink := &logsink.FilesystemSink{BaseDir: t.TempDir()}
	ctx := context.Background()

	ref := logsink.LogRef{OrgID: "org1", RunID: "run1", StepName: "build"}

	// Write first batch.
	if err := sink.Write(ctx, ref, []logsink.LogLine{
		{Timestamp: time.Now(), Stream: "stdout", Content: "line 1"},
	}); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	// Write second batch.
	if err := sink.Write(ctx, ref, []logsink.LogLine{
		{Timestamp: time.Now(), Stream: "stdout", Content: "line 2"},
	}); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	// Read should return both.
	got, err := sink.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(Read()) = %d, want 2", len(got))
	}
}

func TestFilesystemSink_ReadNonExistent(t *testing.T) {
	sink := &logsink.FilesystemSink{BaseDir: t.TempDir()}
	ctx := context.Background()

	ref := logsink.LogRef{OrgID: "org1", RunID: "run1", StepName: "missing"}

	got, err := sink.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if got != nil {
		t.Errorf("Read() = %v, want nil for non-existent log", got)
	}
}

func TestFilesystemSink_MatrixKey(t *testing.T) {
	sink := &logsink.FilesystemSink{BaseDir: t.TempDir()}
	ctx := context.Background()

	ref := logsink.LogRef{
		OrgID:     "org1",
		RunID:     "run1",
		StepName:  "test",
		MatrixKey: "go-1.23-linux",
	}

	if err := sink.Write(ctx, ref, []logsink.LogLine{
		{Timestamp: time.Now(), Stream: "stdout", Content: "matrix run"},
	}); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	got, err := sink.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(Read()) = %d, want 1", len(got))
	}
}

func TestFilesystemSink_Tail(t *testing.T) {
	sink := &logsink.FilesystemSink{BaseDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ref := logsink.LogRef{OrgID: "org1", RunID: "run1", StepName: "tail-test"}

	// Start tailing.
	ch, err := sink.Tail(ctx, ref)
	if err != nil {
		t.Fatalf("Tail() error: %v", err)
	}

	// Write after a short delay.
	go func() {
		time.Sleep(600 * time.Millisecond) // wait for first poll
		_ = sink.Write(context.Background(), ref, []logsink.LogLine{
			{Timestamp: time.Now(), Stream: "stdout", Content: "tailed line"},
		})
	}()

	// Read from channel.
	select {
	case line := <-ch:
		if line.Content != "tailed line" {
			t.Errorf("tailed Content = %q, want %q", line.Content, "tailed line")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for tailed line")
	}
}

func TestLogRef_Path(t *testing.T) {
	tests := []struct {
		name string
		ref  logsink.LogRef
		want string
	}{
		{
			name: "without matrix",
			ref:  logsink.LogRef{OrgID: "org1", RunID: "run1", StepName: "test"},
			want: "org1/run1/test",
		},
		{
			name: "with matrix",
			ref:  logsink.LogRef{OrgID: "org1", RunID: "run1", StepName: "test", MatrixKey: "go-1.23"},
			want: "org1/run1/test/go-1.23",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ref.Path(); got != tt.want {
				t.Errorf("Path() = %q, want %q", got, tt.want)
			}
		})
	}
}
