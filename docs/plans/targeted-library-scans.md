# Targeted Jellyfin Library Scans

## Problem

Every Arrgo operation (scan, import, dedup) triggers `POST /Library/Refresh` which rescans the **entire** Jellyfin library. With a large library this is slow and wasteful when only a single show or movie changed.

## Goal

Refresh only the specific item that changed in Jellyfin, falling back to a full refresh when necessary.

## Jellyfin API

### Item Lookup

Jellyfin doesn't use TVDB/TMDB IDs as primary keys. We need to resolve Arrgo's IDs to Jellyfin item IDs first.

**Search by provider ID:**
```
GET /Items?AnyProviderIdEquals=tvdb.{tvdb_id}
GET /Items?AnyProviderIdEquals=tmdb.{tmdb_id}
GET /Items?AnyProviderIdEquals=imdb.{imdb_id}
```

Returns items with a `Id` field (Jellyfin's internal GUID).

**Search by path (fallback):**
```
GET /Items?Path=/data/shows/Show Name (2020) {tvdb-12345}
```

### Targeted Refresh

Once we have the Jellyfin item ID:
```
POST /Items/{jellyfinItemId}/Refresh
Content-Type: application/json

{
  "MetadataRefreshMode": "Default",
  "ImageRefreshMode": "Default",
  "ReplaceAllMetadata": false,
  "ReplaceAllImages": false
}
```

This only rescans that specific item and its children (seasons/episodes for a show).

## Implementation

### Phase 1: ID Resolution + Caching

Add a `jellyfin_id` column to `shows` and `movies` tables:

```sql
ALTER TABLE shows ADD COLUMN jellyfin_id VARCHAR(100);
ALTER TABLE movies ADD COLUMN jellyfin_id VARCHAR(100);
```

Add a resolution function in `jellyfin.go`:

```go
func ResolveJellyfinItemID(cfg *config.Config, providerType, providerID string) (string, error) {
    // GET /Items?AnyProviderIdEquals={providerType}.{providerID}
    // Parse response, return Jellyfin item ID
    // Returns "" if not found (item hasn't been scanned by Jellyfin yet)
}
```

### Phase 2: Targeted Refresh Function

```go
func RefreshJellyfinItem(cfg *config.Config, jellyfinItemID string) error {
    // POST /Items/{jellyfinItemID}/Refresh with default options
}
```

### Phase 3: Replace Full Refresh Calls

Update `TriggerJellyfinRefresh` to accept an optional item reference:

```go
func TriggerJellyfinRefresh(cfg *config.Config, reason string, opts ...JellyfinRefreshOpts) {
    go func() {
        if len(opts) > 0 && opts[0].ItemID != "" {
            // Try targeted refresh
            if err := RefreshJellyfinItem(cfg, opts[0].ItemID); err == nil {
                return
            }
            // Fall through to full refresh on failure
        }
        RefreshJellyfinLibrary(cfg)
    }()
}
```

**Call sites and what changes:**

| Location | Current | With targeted scan |
|---|---|---|
| `RenameAndMoveShow` | N/A (triggers via scan) | Refresh specific show by jellyfin_id |
| `RenameAndMoveMovie` | N/A (triggers via scan) | Refresh specific movie by jellyfin_id |
| `ScanMovies` / `ScanShows` | Full refresh | Full refresh (correct - bulk operation) |
| `DeduplicateMovies` / `DeduplicateShows` | Full refresh | Full refresh (correct - bulk operation) |
| `ImportAllMoviesHandler` / `ImportAllShowsHandler` | Full refresh | Full refresh (correct - bulk operation) |

The biggest win is **individual imports/renames** which currently trigger a full scan via the scan pipeline. Moving the refresh to the rename functions with targeted scans means Jellyfin updates in seconds instead of minutes.

### Phase 4: Populate jellyfin_id on Existing Library

Run a one-time migration that queries Jellyfin for all library items and matches them to Arrgo's DB by provider IDs:

```go
func SyncJellyfinItemIDs(cfg *config.Config) error {
    // GET /Items?IncludeItemTypes=Series&Recursive=true&Fields=ProviderIds
    // GET /Items?IncludeItemTypes=Movie&Recursive=true&Fields=ProviderIds
    // Match by tvdb_id/tmdb_id/imdb_id -> update jellyfin_id in DB
}
```

This could be an admin action or run automatically on startup.

## Considerations

- **New items**: When a show/movie is imported for the first time, Jellyfin doesn't know about it yet so there's no jellyfin_id. Fall back to full library refresh for new items, then resolve the ID after Jellyfin picks it up.
- **Jellyfin downtime**: All calls already gracefully no-op when Jellyfin isn't configured. Targeted refresh should similarly handle connection failures without breaking Arrgo operations.
- **Rate limiting**: Individual renames during a bulk import would fire many targeted refreshes. For bulk operations, keep using the single full refresh at the end.
