#!/usr/bin/env python3
"""Stop guard: fail if any Go file is not gofmt-formatted.

CI rejects the build when `gofmt -l .` reports any file. The PostToolUse hook
runs `gofmt -w .` after every Edit/Write, but files touched another way (a shell
command, a tool, a git operation) can still drift. This catches that drift at
the end of a turn, before the work is handed back.

Exit code 2 blocks the stop and feeds stderr back to the model so it can run
`gofmt -w .` and continue. We never block on missing/erroring gofmt.
"""
import subprocess
import sys


def main() -> int:
    try:
        result = subprocess.run(
            ["gofmt", "-l", "."],
            capture_output=True,
            text=True,
        )
    except (OSError, ValueError):
        # gofmt unavailable: don't get in the way.
        return 0

    if result.returncode != 0:
        # gofmt itself failed (syntax error, etc.); let other tooling report it.
        return 0

    unformatted = result.stdout.strip()
    if unformatted:
        sys.stderr.write(
            "These files are not gofmt-formatted (CI will reject them):\n"
            f"{unformatted}\n"
            "Run `gofmt -w .` before finishing.\n"
        )
        return 2

    return 0


if __name__ == "__main__":
    sys.exit(main())
