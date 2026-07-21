#!/bin/sh
# clean.sh [N] — delete pingping probe data older than N days (default 30).
# Data files are plain per-day JSONL under ./data/<target>/YYYY-MM-DD.jsonl,
# so cleanup is just find+delete. Run from the pingping directory.
days="${1:-30}"
find ./data -type f -name '20*.jsonl' -mtime +"$days" -print -delete
