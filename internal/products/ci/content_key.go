package ci

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sort"
	"strconv"
)

// DeriveContentKey computes a deterministic cache key from a job's resolved
// declared inputs: the content hashes of its input files, the values of the
// upstream outputs it consumes, and the values of the env vars it reads. This is
// the "big bet" primitive — the author declares inputs, the engine derives the
// key, so a hand-written key can never drift out of sync with what the job
// actually depends on.
//
// Determinism is the whole point (a wrong-but-stable key silently serves stale
// results; a nondeterministic key defeats caching): every map is serialized in
// sorted-key order into a length-prefixed, injection-safe form before hashing.
// The three maps are namespaced so a file named "X" and an env var "X" can never
// alias. An empty input set yields a stable well-known key rather than hashing
// nothing.
//
// Inputs are already RESOLVED — fileHashes come from hashFiles on the agent,
// outputVals from the upstream jobs' recorded outputs, envVals from the run
// environment. Pure: reads no files and no clock, so it is exhaustively testable
// and identical on every worker.
func DeriveContentKey(fileHashes, outputVals, envVals map[string]string) string {
	h := sha256.New()
	writeNamespace(h, "files", fileHashes)
	writeNamespace(h, "needs", outputVals)
	writeNamespace(h, "env", envVals)
	return "content-" + hex.EncodeToString(h.Sum(nil))[:32]
}

// writeNamespace writes a namespace label and its sorted key/value pairs.
func writeNamespace(h io.Writer, ns string, m map[string]string) {
	writeField(h, ns)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	writeField(h, strconv.Itoa(len(keys)))
	for _, k := range keys {
		writeField(h, k)
		writeField(h, m[k])
	}
}

// writeField writes "<len>:<data>" so field boundaries can't be forged by
// embedding separators in a key or value.
func writeField(h io.Writer, s string) {
	_, _ = io.WriteString(h, strconv.Itoa(len(s)))
	_, _ = io.WriteString(h, ":")
	_, _ = io.WriteString(h, s)
}
