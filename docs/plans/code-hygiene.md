# Plan: Code Hygiene & Style Fixes

## Priority: Medium — Could be fixed, not urgent

These are smaller issues that don't affect correctness but move the code closer to idiomatic Go. Good candidates for cleanup PRs when you're already touching nearby code.

---

## 1. Consistent import grouping

**Problem:** Imports are sometimes unordered. For example, `services/movies.go` has local imports before stdlib:

```go
import (
    "Arrgo/config"
    "Arrgo/database"
    "context"
    "database/sql"
    ...
)
```

**Fix:** Standard Go convention is three groups separated by blank lines:
1. Standard library
2. External dependencies
3. Local/internal packages

```go
import (
    "context"
    "database/sql"
    "fmt"

    "github.com/go-chi/chi/v5"

    "Arrgo/config"
    "Arrgo/database"
)
```

**How:** Run `goimports` (or configure your editor to run it on save). This is a one-time bulk fix.

---

## 2. Drop `Get` prefix on getters

**Problem:** Several methods use `GetName()`, `GetMovies()`, etc.

**Fix:** Idiomatic Go drops the `Get` prefix for simple getters:
- `GetName()` -> `Name()`
- `GetIndexers()` -> `Indexers()`
- `GetCurrentUser()` -> `CurrentUser()`
- `GetMovies()` -> `Movies()` (or keep if it implies a DB fetch — judgment call)

The `Get` prefix is a Java/C# convention. In Go, a getter for field `name` is just `Name()`, not `GetName()`.

**Caveat:** For functions that do real work (DB queries, API calls), a verb prefix is fine — `FetchMovies()`, `ListMovies()`, `LoadConfig()`. The issue is specifically with simple accessors.

---

## 3. Mixed `log` and `slog`

**Problem:** Handler `init()` functions use `log.Fatal()` while everything else uses `slog`. Two logging systems in one binary.

**Fix:** Remove the `log` import entirely. If you need fatal-on-startup behavior, use `slog.Error()` + `os.Exit(1)`, or better yet, return errors from template parsing (see dependency-injection plan).

---

## 4. `GetIndexers()` returns an error that's always nil

**Problem:**
```go
func GetIndexers() ([]Indexer, error) {
    return getDefaultIndexers(), nil
}
```

Every caller has to handle an error that can never happen.

**Fix:** Either return just `[]Indexer`, or make it actually return an error when you add configurable/external indexers. Right now it's noise.

---

## 5. Long functions

**Problem:** Several functions (`ScanMovies`, `ScanShows`, `processMovieDir`, various handlers) are 100-200+ lines. Go prefers shorter, composable functions.

**Fix:** Extract logical sections into named helper functions. For example in `ScanMovies`:
- File discovery -> `discoverMovieDirs(path string) ([]string, error)`
- Worker pool setup -> could be a generic helper if the pattern repeats
- Processing a single directory -> already extracted to `processMovieDir` (good)

Don't over-extract. If a block of code is only used once and is easy to read in place, leave it. The goal is readability, not arbitrary line count targets.

---

## 6. Template `init()` functions

**Problem:** Every handler file has an `init()` that parses templates and calls `log.Fatal` on failure. `init()` runs before `main()`, so you can't control error handling, logging isn't initialized yet, and it makes the startup order implicit.

**Fix:** Parse templates in an explicit setup function called from `main()`:
```go
func NewMovieHandler(cfg *config.Config) (*MovieHandler, error) {
    tmpl, err := template.New("movies").Funcs(GetFuncMap()).ParseFiles(...)
    if err != nil {
        return nil, fmt.Errorf("parsing movies template: %w", err)
    }
    return &MovieHandler{templates: tmpl}, nil
}
```

This ties naturally into the dependency injection plan.

---

## 7. Error messages starting with uppercase

**Problem (minor):** Some error strings start with a capital letter: `"Movie scan already in progress"`. Go convention is lowercase error strings since they're often wrapped: `fmt.Errorf("scanning: %w", err)` reads poorly as `"scanning: Movie scan already in progress"`.

**Fix:** Lowercase the first letter of error messages. Log messages can stay uppercase — this only applies to returned `error` values.

---

## Non-issues (leave as-is)

- **chi router choice** — fine, no reason to change.
- **No ORM** — this is a feature, not a bug. Raw SQL with `database/sql` is perfectly idiomatic.
- **Server-side rendering with `html/template`** — totally fine for this kind of app.
- **Module-per-service layout** (server, indexer, ffsubsync-api as separate modules) — reasonable for independent deployment.
