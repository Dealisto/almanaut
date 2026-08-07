// Command almanaut-agent collects this machine's facts and reports them to an
// almanaut server. It is a one-shot program driven by a systemd timer, not a
// daemon: a process that lives two seconds cannot leak memory, and a non-zero
// exit surfaces in `systemctl list-timers` and the journal, so observability
// costs nothing.
package main

import (
	"context"
	"os"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
