#!/usr/bin/env python3
"""PreToolUse guard: keep SQL migrations append-only.

Claude Code passes the tool invocation as JSON on stdin. We block any Edit or
Write that targets an *existing* .sql file under internal/store/migrations/,
because applied migrations must never change (it would corrupt databases that
already ran them). Creating a brand-new numbered migration is still allowed.

Exit code 2 tells Claude Code to block the tool call and feeds stderr back to
the model as the reason.
"""
import json
import os
import sys

MIGRATIONS_DIR = "internal/store/migrations/"


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

    normalized = file_path.replace("\\", "/")
    is_migration = MIGRATIONS_DIR in normalized and normalized.endswith(".sql")
    if is_migration and os.path.exists(file_path):
        sys.stderr.write(
            "Blocked: migrations are append-only.\n"
            f"'{file_path}' has already been committed and may have run against "
            "existing databases, so editing it would corrupt them.\n"
            "Create a new migration instead: add the next numbered file "
            "(e.g. internal/store/migrations/0009_<name>.sql) with the change.\n"
        )
        return 2

    return 0


if __name__ == "__main__":
    sys.exit(main())
