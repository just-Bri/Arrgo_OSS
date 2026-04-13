# Scan / Rename / Dedupe Bug Tracker

Identified during code review of `services/movies.go`, `services/shows.go`, `services/renamer.go`, `services/dedupe.go`.

---

## Bug 1 — `cleanTitleTags` destroys titles containing "MOVIE" `[x]`

**File:** `server/services/renamer.go` — `cleanTitleTags()`

**Problem:** The tag list includes `"MOVIE"` and `"THE MOVIE"`. The regex built is:
```
(?i)\s*[-_.]?\s*MOVIE\b
```
`\b` is a word boundary, not a full-word anchor. "Movie 43" → `"43"`. "Movie" → `""`. Any film with "Movie" in its title gets mangled. Same logic applies to other tags that are also real words (e.g. a title containing "Sub" or "Dub").

**Fix:** Remove `"MOVIE"` and `"THE MOVIE"` from the tags list. These appear as scene release suffixes only when preceded by a separator and not as standalone title words — the regex isn't strict enough to safely catch only those cases.

---

## Bug 2 — Episode rescan goroutine ignores case-insensitive path resolution `[x]`

**File:** `server/services/renamer.go` — `renameAndMoveEpisodeInternal()` (rescan goroutine near bottom)

**Problem:** The parent function resolves the actual on-disk path using `findExistingDirCaseInsensitive` and stores it in `showDirPath`. The goroutine rebuilds the path from `showDirName` without that resolution:
```go
go func() {
    showDirPath := filepath.Join(cfg.ShowsPath, showDirName) // raw, unresolved
    scanSeasons(sh.ID, showDirPath, sh.Title)
}()
```
If the folder on disk is `"Bofuri"` but `showDirName` is `"BOFURI"`, the rescan targets a nonexistent path and silently does nothing. Episodes imported under that show will not appear in the next scan.

**Fix:** Capture the already-resolved `showDirPath` (the local var computed earlier in the function) in the goroutine closure instead of recomputing from `showDirName`.

---

## Bug 3 — `mergeShowFolders` deletes source file even if copy failed `[x]`

**File:** `server/services/dedupe.go` — `mergeShowFolders()`

**Problem:**
```go
if err := os.Rename(path, destPath); err != nil {
    copyFile(path, destPath) // error ignored
    os.Remove(path)          // runs even if copy failed
}
```
If `copyFile` fails (disk full, permissions, IO error), `os.Remove` still runs. The file is permanently deleted from both source and destination.

**Fix:**
```go
if err := os.Rename(path, destPath); err != nil {
    if copyErr := copyFile(path, destPath); copyErr != nil {
        slog.Error("Failed to copy file during show folder merge", "from", path, "to", destPath, "error", copyErr)
        return nil
    }
    os.Remove(path)
}
```

---

## Bug 4 — Dedupe uses hardcoded extension subset instead of shared `MovieExtensions` map `[x]`

**File:** `server/services/dedupe.go` — `DeduplicateMovies()` and `dedupeEpisodesInShow()`

**Problem:** Both functions check:
```go
if ext == ".mkv" || ext == ".mp4" || ext == ".avi" {
```
The rest of the codebase uses `MovieExtensions[ext]` which covers additional formats (`.m4v`, `.mov`, `.ts`, `.m2ts`, `.wmv`, etc.). Duplicates in those formats are never detected or removed.

**Fix:** Replace both hardcoded extension checks with `MovieExtensions[ext]`.

---

## Bug 5 — `isCleanEnglishEpisodeTitle` rejects valid Latin Extended characters `[x]`

**File:** `server/services/shows.go` — `isCleanEnglishEpisodeTitle()`

**Problem:**
```go
if r > 127 && (r < 0x2000 || r > 0x206F) {
    return false
}
```
`0x2000–0x206F` is the Unicode "General Punctuation" block. Latin Extended characters (`é`, `ñ`, `ü`, `ø`, etc.) live at `0x00C0–0x024F` — above 127 but below `0x2000` — so they trigger this check and return false. Episode titles for French, Spanish, German, and Portuguese shows are rejected, causing incorrect fallback to `"Episode N"`.

**Fix:** The intent is to reject CJK and other non-Latin scripts while accepting Latin Extended:
```go
// Reject if any rune is outside Latin + Latin Extended + common punctuation
if r > 0x024F && !(r >= 0x2000 && r <= 0x206F) {
    return false
}
```

---

## Bug 6 — Episode title heuristic fires on any hyphen, re-processing clean titles `[x]`

**File:** `server/services/renamer.go` — `renameAndMoveEpisodeInternal()`

**Problem:**
```go
if strings.Contains(epTitle, ".") || strings.Contains(epTitle, "-") || strings.Contains(strings.ToLower(epTitle), "s0") {
    epTitle, _, _, _, _ = ParseMediaName(epTitle)
}
```
This runs `ParseMediaName` (which calls `cleanTitleTags`) on any title that contains a hyphen. Legitimate episode titles like `"Spider-Man"`, `"Step-by-Step"`, `"Doctor Who - The Movie"` trigger it. The `"s0"` check is also too broad — any title containing the character sequence `s0` (e.g. `"ISO 9000"`) triggers it.

**Fix:** Only trigger cleanup if the title looks like a raw scene filename — i.e., contains dots as separators or explicit quality/codec tags:
```go
sceneNameRegex := regexp.MustCompile(`(?i)\b(1080p|720p|2160p|480p|WEB-DL|WEBRip|BluRay|BDRip|x264|x265|HEVC|H264)\b`)
if strings.Contains(epTitle, ".") || sceneNameRegex.MatchString(epTitle) {
    epTitle, _, _, _, _ = ParseMediaName(epTitle)
}
```

---

## Bug 7 — `config.Load()` called per-episode inside scan loop `[x]`

**File:** `server/services/shows.go` — `scanEpisodes()` and `scanEpisodesFromShowFolder()`

**Problem:** Both functions call `cfg := config.Load()` once per episode file, re-reading all environment variables from the OS on every iteration. This is wasteful on large libraries.

**Fix:** Pass `cfg *config.Config` as a parameter to both functions (the same way `processShowDir` already receives it) and thread it through from the caller.

---

## Bug 8 — `PurgeMissing*` executes deletes while holding open query cursor `[x]`

**File:** `server/services/movies.go` — `PurgeMissingMovies()` and `server/services/shows.go` — `PurgeMissingShows()`

**Problem:** Both functions issue `DELETE` statements while iterating an open `rows` cursor on the same connection pool. With `database/sql` this is normally safe (uses separate pool connections), but it is fragile and can cause issues if the pool is saturated.

**Fix:** Collect IDs to delete in a slice first, close the cursor, then bulk-delete:
```go
var toDelete []int
for rows.Next() {
    var id int; var path string
    rows.Scan(&id, &path)
    if _, err := os.Stat(path); os.IsNotExist(err) {
        toDelete = append(toDelete, id)
    }
}
rows.Close()
for _, id := range toDelete {
    database.DB.Exec("DELETE FROM movies WHERE id = $1", id)
}
```

---

## Status

| # | Description | Status |
|---|-------------|--------|
| 1 | `cleanTitleTags` nukes "MOVIE" from real titles | `[x]` |
| 2 | Rescan goroutine uses unresolved path | `[x]` |
| 3 | `mergeShowFolders` data loss on copy failure | `[x]` |
| 4 | Dedupe hardcoded extension list | `[x]` |
| 5 | `isCleanEnglishEpisodeTitle` rejects Latin Extended | `[x]` |
| 6 | Episode title heuristic fires on hyphens | `[x]` |
| 7 | `config.Load()` per-episode in scan loop | `[x]` |
| 8 | Purge cursor held during deletes | `[x]` |
