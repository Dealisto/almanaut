#!/usr/bin/env python3
"""PreToolUse guard: block edits to generated/managed files.

Some files in the repo must never be hand-edited:
  * almanaut.exe        - a build artifact (produced by `go build`).
  * go.sum              - managed by the Go toolchain (`go mod tidy`/`go get`),
                          never by hand; a manual edit corrupts checksums.

Claude Code passes the tool invocation as JSON on stdin. Exit code 2 blocks the
tool call and feeds stderr back to the model as the reason.
"""
import json
import os
import sys

# Basenames (lowercased) that must not be edited or overwritten via Edit/Write.
PROTECTED = {
    "almanaut.exe": "a build artifact; rebuild it with `go build` instead of editing it.",
    "go.sum": "managed by the Go toolchain; run `go mod tidy` or `go get` instead of editing it.",
}


def main() -> int:
    try:
        payload = json.load(sys.stdin)
    except (json.JSONDecodeError, ValueError):
        # If we cannot parse the input, don't get in the way.
        return 0

    tool_input = payload.get("tool_input", {}) or {}
    file_path = tool_input.get("file_path") or tool_input.get("path") or ""
    if not file_path:
        return 0

    # Lowercase the basename so the guard also holds on case-insensitive
    # filesystems (Windows/macOS), where "GO.SUM" resolves to the same file.
    base = os.path.basename(file_path.replace("\\", "/")).lower()
    reason = PROTECTED.get(base)
    if reason:
        sys.stderr.write(f"Blocked: '{base}' is {reason}\n")
        return 2

    return 0


if __name__ == "__main__":
    sys.exit(main())
