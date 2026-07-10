// Package job provides a small in-process scheduler: named recurring jobs,
// each run on its own goroutine with a per-job interval, plus manual triggers
// and live status for an admin page. Status is in-memory only and resets on
// restart, rebuilt from the first pass (mirrors kuma.Syncer.LastSync).
package job

import (
	"context"
	"log"
	"sync"
	"time"
)

// defaultTimeout bounds a single pass when a Definition leaves Timeout zero.
const defaultTimeout = time.Minute

// RunFunc is one execution of a job. It must honour ctx cancellation.
type RunFunc func(ctx context.Context) error

// Definition registers a recurring job with the runner.
type Definition struct {
	Name     string        // stable identifier, unique within a runner
	Title    string        // human-readable label for the admin page
	Interval time.Duration // time between passes; <=0 => runs only at startup and on manual trigger
	Timeout  time.Duration // per-pass bound; <=0 => defaultTimeout
	Run      RunFunc
}

// Status is an immutable snapshot of a job's live state.
type Status struct {
	Name     string
	Title    string
	Interval time.Duration
	LastRun  time.Time // zero => never ran
	NextRun  time.Time // zero => not scheduled yet
	LastErr  string    // "" => last pass succeeded (or never ran)
	LastDur  time.Duration
	Running  bool
	Runs     int64
}

type job struct {
	def     Definition
	trigger chan struct{} // size 1: coalesces manual runs

	mu      sync.Mutex
	lastRun time.Time
	nextRun time.Time
	lastErr string
	lastDur time.Duration
	running bool
	runs    int64
}

func (j *job) status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Status{
		Name: j.def.Name, Title: j.def.Title, Interval: j.def.Interval,
		LastRun: j.lastRun, NextRun: j.nextRun, LastErr: j.lastErr,
		LastDur: j.lastDur, Running: j.running, Runs: j.runs,
	}
}

// Runner owns a set of registered jobs and runs them until its context ends.
type Runner struct {
	log  *log.Logger
	mu   sync.Mutex
	jobs []*job
}

// New returns an empty runner. A nil logger defaults to log.Default().
func New(logger *log.Logger) *Runner {
	if logger == nil {
		logger = log.Default()
	}
	return &Runner{log: logger}
}

// Register adds a job. Call before Start. Registration order is the order
// Statuses reports, keeping the admin page stable.
func (r *Runner) Register(d Definition) {
	if d.Timeout <= 0 {
		d.Timeout = defaultTimeout
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = append(r.jobs, &job{def: d, trigger: make(chan struct{}, 1)})
}

// Statuses returns a snapshot of every registered job, in registration order.
func (r *Runner) Statuses() []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Status, len(r.jobs))
	for i, j := range r.jobs {
		out[i] = j.status()
	}
	return out
}
