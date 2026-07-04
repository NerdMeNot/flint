package units

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCPUMillis(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"500m", 500, false},
		{"1", 1000, false},
		{"2", 2000, false},
		{"0.5", 500, false},
		{"2.5", 2500, false},
		{"16", 16000, false},
		{" 4 ", 4000, false},
		{"", 0, true},
		{"-1", 0, true},
		{"-500m", 0, true},
		{"abc", 0, true},
		{"1.5m", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseCPUMillis(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"1Ki", 1024, false},
		{"1Mi", 1 << 20, false},
		{"8Gi", 8 << 30, false},
		{"1Ti", 1 << 40, false},
		{"1G", 1_000_000_000, false},
		{"1K", 1000, false},
		{"1k", 1000, false},
		{"512Mi", 512 << 20, false},
		{"1.5Gi", 1610612736, false},
		{"1024", 1024, false},
		{"", 0, true},
		{"-1Gi", 0, true},
		{"xyz", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseBytes(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseMemoryMB(t *testing.T) {
	got, err := ParseMemoryMB("2Gi")
	require.NoError(t, err)
	assert.Equal(t, int64(2048), got)

	// rounds up — a request is never silently shrunk
	got, err = ParseMemoryMB("1025Ki")
	require.NoError(t, err)
	assert.Equal(t, int64(2), got)
}

func TestParseDiskGB(t *testing.T) {
	got, err := ParseDiskGB("100Gi")
	require.NoError(t, err)
	assert.Equal(t, int64(100), got)

	got, err = ParseDiskGB("1Mi")
	require.NoError(t, err)
	assert.Equal(t, int64(1), got)
}
