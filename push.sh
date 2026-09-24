#!/bin/sh
cd "$(dirname "$0")"
git add -A
git commit -m "Update $(date +%Y-%m-%d)"
git push