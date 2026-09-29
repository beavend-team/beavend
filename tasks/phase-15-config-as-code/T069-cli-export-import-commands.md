# T069 — CLI Export & Import Commands

## Status
Completed

## Objective
Wire CLI subcommands in `cmd/beavend` enabling offline or database-direct configuration export and import.

## Key Changes
1. `cmd/beavend/main.go` subcommands:
   - `beavend export --project=<id> --env=<env> [--output=<file>] [--db-url=<url>]`
   - `beavend import --project=<id> --env=<env> --file=<file> [--dry-run] [--db-url=<url>]`
2. Standalone execution: connects to PostgreSQL, runs goose migrations if needed, executes `configcode.Engine`, and prints formatted output / diff report.
3. Tests validating CLI flag parsing and round-trip export/import.
