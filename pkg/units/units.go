// Package units parses the human-friendly resource quantity strings used in
// pipeline YAML and pool definitions ("500m", "4", "8Gi", "100G") into plain
// integers. It intentionally covers only the subset of Kubernetes quantity
// syntax Flint ever used, so the k8s.io/apimachinery dependency could be
// dropped with the substrate pivot.
package units

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseCPUMillis parses a CPU quantity into millicores: "500m" → 500,
// "2" → 2000, "0.5" → 500.
func ParseCPUMillis(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("units: empty cpu quantity")
	}
	if millis, ok := strings.CutSuffix(s, "m"); ok {
		n, err := strconv.ParseInt(millis, 10, 64)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("units: invalid cpu quantity %q", s)
		}
		return n, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, fmt.Errorf("units: invalid cpu quantity %q", s)
	}
	return int64(math.Round(f * 1000)), nil
}

// suffix multipliers in bytes. Binary (Ki) and decimal (K/k) forms, matching
// the subset of Kubernetes quantity syntax that appears in Flint configs.
var byteSuffixes = []struct {
	suffix string
	mult   float64
}{
	{"Pi", 1 << 50}, {"Ti", 1 << 40}, {"Gi", 1 << 30}, {"Mi", 1 << 20}, {"Ki", 1 << 10},
	{"P", 1e15}, {"T", 1e12}, {"G", 1e9}, {"M", 1e6}, {"K", 1e3}, {"k", 1e3},
}

// ParseBytes parses a memory/disk quantity into bytes: "8Gi" → 8589934592,
// "1G" → 1000000000, "512Mi", plain integers are bytes.
func ParseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("units: empty byte quantity")
	}
	num, mult := s, float64(1)
	for _, sfx := range byteSuffixes {
		if rest, ok := strings.CutSuffix(s, sfx.suffix); ok {
			num, mult = rest, sfx.mult
			break
		}
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f < 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, fmt.Errorf("units: invalid byte quantity %q", s)
	}
	return int64(math.Round(f * mult)), nil
}

// ParseMemoryMB parses a memory quantity and returns whole mebibytes,
// rounding up so a request is never silently shrunk.
func ParseMemoryMB(s string) (int64, error) {
	b, err := ParseBytes(s)
	if err != nil {
		return 0, err
	}
	return ceilDiv(b, 1<<20), nil
}

// ParseDiskGB parses a disk quantity and returns whole gibibytes, rounding up.
func ParseDiskGB(s string) (int64, error) {
	b, err := ParseBytes(s)
	if err != nil {
		return 0, err
	}
	return ceilDiv(b, 1<<30), nil
}

func ceilDiv(n, d int64) int64 {
	if n == 0 {
		return 0
	}
	return (n + d - 1) / d
}
