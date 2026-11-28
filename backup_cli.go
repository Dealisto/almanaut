package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/Dealisto/almanaut/internal/backup"
)

// runBackupCommand runs independently of server configuration and startup. args
// begins with "backup" or "backup-verify"; neither command initializes a server.
func runBackupCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "backup" && args[0] != "backup-verify") {
		fmt.Fprintln(stderr, "expected backup or backup-verify")
		return 2
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	source := flags.String("source", "", "existing Almanaut database path (required)")
	timeout := flags.Duration("timeout", 10*time.Minute, "maximum operation duration")
	var destination string
	if args[0] == "backup" {
		flags.StringVar(&destination, "destination", "", "new database backup path (required; never overwritten)")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *source == "" || *timeout <= 0 || (args[0] == "backup" && destination == "") {
		fmt.Fprintln(stderr, "provide --source, a positive --timeout, and --destination for backup; positional arguments are not accepted")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	var info backup.Verification
	var err error
	if args[0] == "backup" {
		info, err = backup.Create(ctx, *source, destination)
	} else {
		info, err = backup.Verify(ctx, *source)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if args[0] == "backup" {
		fmt.Fprintf(stdout, "Database backup created and verified: %s\n", destination)
		fmt.Fprintln(stdout, "External configuration and secret files are not included.")
	} else {
		fmt.Fprintf(stdout, "Database integrity and Almanaut schema verified: %s\n", *source)
		fmt.Fprintln(stdout, "This check does not prove restore compatibility or test application login and attachments.")
	}
	fmt.Fprintf(stdout, "Schema: %s (%d migrations); size: %d bytes\n", info.SchemaVersion, info.Migrations, info.Bytes)
	return 0
}
