package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/agent"
)

// version is the agent's reported version. The release build overrides it with
// -ldflags "-X main.version=...".
var version = "dev"

// Exit codes. They are chosen so `systemctl list-timers` and the journal can
// distinguish a setup problem a human must fix from a transient failure the
// next run will resolve on its own.
const (
	exitOK          = 0
	exitUsage       = 1 // bad flags, or unreadable/incomplete config
	exitRejected    = 2 // 4xx — a bad token or an unsupported schema
	exitUnreachable = 3 // transport failure or 5xx — retry next hour
	exitConflict    = 4 // 409 — a human must run reset-id on the clone
)

// run is the whole program. main only supplies the context and writers, so
// every path here is exercised end-to-end in tests against an httptest server.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("almanaut-agent", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", agent.DefaultConfigPath, "path to the agent config file")
	stateDir := fs.String("state-dir", agent.DefaultStateDir, "directory holding the persistent agent id")
	timeout := fs.Duration("timeout", 30*time.Second, "overall deadline for collecting and reporting")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: almanaut-agent [flags] [report|reset-id|version]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	switch cmd := fs.Arg(0); cmd {
	case "", "report":
		return doReport(ctx, *configPath, *stateDir, *timeout, stdout, stderr)
	case "reset-id":
		// Deliberately independent of the config: an operator runs this on a
		// clone exactly when reporting is broken.
		id, err := agent.ResetAgentID(*stateDir)
		if err != nil {
			fmt.Fprintf(stderr, "almanaut-agent: %v\n", err)
			return exitUsage
		}
		fmt.Fprintln(stdout, id)
		return exitOK
	case "version":
		fmt.Fprintln(stdout, version)
		return exitOK
	default:
		fmt.Fprintf(stderr, "almanaut-agent: unknown command %q\n", cmd)
		fs.Usage()
		return exitUsage
	}
}

func doReport(ctx context.Context, configPath, stateDir string, timeout time.Duration, stdout, stderr io.Writer) int {
	cfg, err := agent.LoadConfig(configPath, os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "almanaut-agent: %v\n", err)
		return exitUsage
	}
	id, err := agent.LoadOrCreateAgentID(stateDir)
	if err != nil {
		fmt.Fprintf(stderr, "almanaut-agent: %v\n", err)
		return exitUsage
	}
	hostname, err := os.Hostname()
	if err != nil {
		fmt.Fprintf(stderr, "almanaut-agent: read hostname: %v\n", err)
		return exitUsage
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	root := agent.SystemRoot()
	rep := agent.Collect(agent.CollectOptions{
		Root:         root,
		AgentID:      id,
		AgentVersion: version,
		Hostname:     hostname,
		Interfaces:   agent.SystemInterfaces,
		Usage:        agent.SystemDiskUsage,
	})

	res, err := agent.Send(ctx, &http.Client{Timeout: timeout}, cfg.ServerURL, cfg.Token, rep)
	switch {
	case err == nil:
		changed := "nothing"
		if len(res.Changed) > 0 {
			changed = strings.Join(res.Changed, ", ")
		}
		fmt.Fprintf(stdout, "reported host %d; changed: %s\n", res.HostID, changed)
		return exitOK
	case errors.Is(err, agent.ErrConflict):
		fmt.Fprintf(stderr, "almanaut-agent: %v\n", err)
		return exitConflict
	case errors.Is(err, agent.ErrRejected):
		fmt.Fprintf(stderr, "almanaut-agent: %v\n", err)
		return exitRejected
	default:
		fmt.Fprintf(stderr, "almanaut-agent: %v\n", err)
		return exitUnreachable
	}
}
