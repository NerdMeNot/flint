package agent

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// emitFilePath is where the step container writes emit KEY VALUE pairs.
// The agent injects an `emit` shell function that appends to this file.
const emitFilePath = "/workspace/.flint-emit"

// EmitScript returns the shell function that the agent injects into the
// step container's environment. Steps call `emit KEY VALUE` to set outputs.
func EmitScript() string {
	return fmt.Sprintf(`emit() { echo "$1=$2" >> %s; }`, emitFilePath)
}

// ReadEmits reads all emit KEY=VALUE pairs written by the step container.
func ReadEmits(workspace string) (map[string]string, error) {
	path := filepath.Join(workspace, ".flint-emit")

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no emits — valid
		}
		return nil, fmt.Errorf("agent: failed to read emit file: %w", err)
	}
	defer f.Close()

	outputs := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			outputs[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	return outputs, scanner.Err()
}
