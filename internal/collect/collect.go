// Package collect gathers machine inventory into a snapshot. Each collector is
// failure-isolated and they run concurrently.
package collect

import (
	"context"
	"sync"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
	"github.com/doguyilmaz/dothaven/internal/sys"
)

// Ctx carries the shared inputs for a collection run.
type Ctx struct {
	Context context.Context
	Env     sys.Env
	Home    string
	Redact  bool
}

// A Collector gathers some sections. It must not abort the run on failure — it
// returns whatever it could (an empty map is fine), and panics are recovered.
type Collector func(c Ctx) snapshot.Snapshot

// toItems builds single-column items from names (the common item shape).
func toItems(names []string) []snapshot.Item {
	out := make([]snapshot.Item, len(names))
	for i, n := range names {
		out[i] = snapshot.Item{Raw: n, Columns: []string{n}}
	}
	return out
}

func ptr(s string) *string { return &s }

// RunCollectors runs every collector concurrently (failure-isolated: a panic or
// empty return from one never affects the others) and merges their sections.
// Results merge in collector order, so a later collector wins a key conflict.
func RunCollectors(c Ctx, collectors []Collector) snapshot.Snapshot {
	results := make([]snapshot.Snapshot, len(collectors))
	var wg sync.WaitGroup
	for i, col := range collectors {
		wg.Add(1)
		go func(i int, col Collector) {
			defer wg.Done()
			defer func() { _ = recover() }()
			results[i] = col(c)
		}(i, col)
	}
	wg.Wait()

	merged := snapshot.Snapshot{}
	for _, r := range results {
		for k, v := range r {
			merged[k] = v
		}
	}
	return merged
}
