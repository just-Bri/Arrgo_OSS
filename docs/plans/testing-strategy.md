# Plan: Add a Testing Strategy

## Priority: High — Needs to be fixed

Zero test files in a codebase with this much business logic (scanning, deduplication, quality detection, renaming, metadata matching) is a real risk. Any of these can silently break.

## Problem

There are no `*_test.go` files anywhere in the project. The global state problem (see `dependency-injection.md`) is both the cause and a blocker — you can't easily test services that reach into `database.DB` directly.

This plan assumes dependency injection lands first, or at minimum is done in parallel for the areas being tested.

## What to test (in priority order)

### Tier 1: Pure logic — no dependencies, high value

These are free wins. No mocking, no test databases, no setup.

1. **Quality detection** (`quality.go`) — regex-based parsing, deterministic input/output. Table-driven tests with filenames and expected quality levels.
2. **Filename/folder parsing** — TMDB ID extraction, season/episode regex, title cleanup. These are the core of your scan logic and regressions here silently corrupt your library.
3. **Deduplication logic** — given two files with qualities X and Y, which one wins? Edge cases: same quality, unknown quality, missing files.
4. **Size formatting, string utilities** (`shared/format/`) — trivial to test.

### Tier 2: Service logic with a test database

5. **Movie/show scanning** — given a mock filesystem (use `os.MkdirTemp`), does the scanner find the right files, skip the right dirs, insert the right records?
6. **Request lifecycle** — create, complete, cleanup. Verify the state machine works.
7. **Subtitle queue** — deduplication, processing order.

For these, use a real test PostgreSQL instance (docker-compose or testcontainers-go). Do not mock `*sql.DB` — the SQL queries are the thing most likely to break.

### Tier 3: Integration / HTTP tests

8. **Handler tests** — use `httptest.NewServer` with real (test-database-backed) services. Verify redirects, auth enforcement, correct template rendering.
9. **Indexer tests** — these hit external sites so they should be opt-in (`-tags=integration` or `if os.Getenv("RUN_INTEGRATION") != ""`).

## Steps

1. **Start with quality detection tests.** Create `server/services/quality_test.go` with table-driven tests. This should take 30 minutes and immediately proves value.
2. **Add filename parsing tests.** These cover the most fragile, regex-heavy code paths.
3. **Set up a test database.** Add a `testutil` package or use `TestMain` to spin up/tear down a test schema. Use the same `migrations.go` schema init.
4. **Write service tests** for scanning and deduplication against the test DB.
5. **Add CI.** Even just `go test ./...` in a GitHub Action with a Postgres service container.

## Conventions to follow

- Table-driven tests with `t.Run` subtests — this is the Go standard.
- Test files live next to the code they test (`quality_test.go` next to `quality.go`).
- Use `testdata/` directories for fixture files if needed.
- No test frameworks (testify, gomega, etc.) — `testing` + `if got != want` is idiomatic. If you really want helpers, `testify/assert` is the most common, but it's not necessary.

## Example starting point

```go
// services/quality_test.go
package services

import "testing"

func TestDetectQuality(t *testing.T) {
    tests := []struct {
        filename string
        want     string
    }{
        {"Movie.Name.2024.2160p.BluRay.x265", Quality4K},
        {"Movie.Name.2024.1080p.WEB-DL", Quality1080p},
        {"Movie.Name.2024.720p.HDTV", Quality720p},
        {"Movie.Name.2024.DVDRip", QualitySD},
        {"Movie.Name.2024", QualityUnknown},
    }
    for _, tt := range tests {
        t.Run(tt.filename, func(t *testing.T) {
            got := DetectQuality(tt.filename)
            if got != tt.want {
                t.Errorf("DetectQuality(%q) = %q, want %q", tt.filename, got, tt.want)
            }
        })
    }
}
```
