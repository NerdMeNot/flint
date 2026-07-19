package ci

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ExpandFanOut materializes a runtime fan-out: given a job template with a
// fanOut: expression and the JSON array that expression evaluated to at runtime,
// it produces one concrete child job per element. Each child is named
// "<job>[i]", has its fanOut cleared, and carries the element in the environment
// as FLINT_FANOUT_ITEM (the raw string for a string element, compact JSON
// otherwise) plus FLINT_FANOUT_INDEX — so the job's steps read their shard from
// the environment without any expression plumbing.
//
// Fan-in is the existing needs graph: a job that `needs` the fan-out parent
// waits for every "<job>[i]" (the engine rewrites the edge, exactly as it does
// for matrix variants). This function is the pure expansion core — the engine
// evaluates the array (fromJSON on an upstream output) and calls this; keeping
// it pure makes the sharding logic exhaustively testable without a running
// workflow. An empty array yields zero children (the parent collapses to a
// no-op, and its dependents proceed).
func ExpandFanOut(name string, job Job, items []any) map[string]Job {
	out := make(map[string]Job, len(items))
	for i, item := range items {
		child := job
		child.FanOut = ""

		env := make(map[string]string, len(job.Env)+2)
		for k, v := range job.Env {
			env[k] = v
		}
		env["FLINT_FANOUT_ITEM"] = fanoutItemString(item)
		env["FLINT_FANOUT_INDEX"] = strconv.Itoa(i)
		child.Env = env

		out[fmt.Sprintf("%s[%d]", name, i)] = child
	}
	return out
}

// fanoutItemString renders one array element for the environment: a string
// element passes through verbatim (the common case — a list of shard names);
// anything else is compact JSON so an object/number survives intact.
func fanoutItemString(item any) string {
	if s, ok := item.(string); ok {
		return s
	}
	b, err := json.Marshal(item)
	if err != nil {
		return fmt.Sprintf("%v", item)
	}
	return string(b)
}
