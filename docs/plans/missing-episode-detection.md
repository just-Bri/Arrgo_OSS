# Missing Episode Detection

## Problem

Episodes can go missing from either side:
- **Missing in Jellyfin**: File exists on disk and in Arrgo's DB, but Jellyfin didn't pick it up (scan issue, permissions, unsupported format).
- **Missing in Arrgo**: Jellyfin has the episode but Arrgo's DB lost track of it (DB nuke, failed scan, manual file add).
- **Missing on disk**: Both DBs reference an episode but the file was deleted or moved.

Currently there's no way to detect these gaps without manually checking.

## Goal

Compare Arrgo's episode/movie DB against Jellyfin's library and report discrepancies. Optionally auto-fix what can be fixed.

## Jellyfin API

### Get All Shows
```
GET /Items?IncludeItemTypes=Series&Recursive=true&Fields=ProviderIds
```

Returns all series with their provider IDs (TVDB, TMDB, IMDB) for matching against Arrgo's DB.

### Get Episodes for a Show
```
GET /Shows/{jellyfinSeriesId}/Episodes?Fields=Path,ProviderIds&SeasonId={seasonId}
```

Or get all episodes at once:
```
GET /Items?ParentId={jellyfinSeriesId}&IncludeItemTypes=Episode&Recursive=true&Fields=Path,ProviderIds
```

Returns episodes with `Path`, `IndexNumber` (episode num), `ParentIndexNumber` (season num).

### Get All Movies
```
GET /Items?IncludeItemTypes=Movie&Recursive=true&Fields=Path,ProviderIds
```

## Implementation

### Data Structure

```go
type MissingEpisodeReport struct {
    ShowTitle     string
    ShowTVDBID    string
    InArrgoOnly   []EpisodeRef  // In Arrgo DB but not in Jellyfin
    InJellyfinOnly []EpisodeRef // In Jellyfin but not in Arrgo DB
    FileMissing   []EpisodeRef  // In both DBs but file doesn't exist on disk
}

type EpisodeRef struct {
    Season  int
    Episode int
    Title   string
    Path    string
}

type MissingMovieReport struct {
    InArrgoOnly    []MovieRef
    InJellyfinOnly []MovieRef
    FileMissing    []MovieRef
}
```

### Core Function

```go
func DetectMissingEpisodes(cfg *config.Config) ([]MissingEpisodeReport, error) {
    // 1. Fetch all series from Jellyfin with provider IDs
    // 2. For each Arrgo show, find matching Jellyfin series by tvdb_id/tmdb_id
    // 3. Fetch episodes from both sources
    // 4. Compare by season_number + episode_number
    // 5. Check file existence on disk for matches
    // 6. Build report of discrepancies
}

func DetectMissingMovies(cfg *config.Config) (*MissingMovieReport, error) {
    // 1. Fetch all movies from Jellyfin with provider IDs
    // 2. Match against Arrgo movies by tmdb_id/imdb_id
    // 3. Check file existence on disk
    // 4. Build report
}
```

### Matching Strategy

Match shows/movies between Arrgo and Jellyfin by provider IDs in priority order:

1. **TVDB ID** (shows) / **TMDB ID** (movies) — most reliable
2. **IMDB ID** — fallback
3. **Normalized title + year** — last resort for items without provider IDs

For episodes within a matched show, compare by `(season_number, episode_number)` tuple.

### API Endpoint

```
GET /api/admin/jellyfin/missing-episodes
GET /api/admin/jellyfin/missing-movies
```

Returns the report as JSON. Admin only.

### UI

Add a section to the admin panel showing:
- Count of discrepancies per show
- Expandable list showing which episodes are missing where
- Action buttons:
  - "Rescan in Jellyfin" — triggers targeted refresh for that show
  - "Rescan in Arrgo" — triggers Arrgo's show scan for that path
  - "Remove from DB" — cleans up orphaned DB entries where file is gone

### Auto-Fix Options

Some issues can be auto-resolved:

| Issue | Auto-fix |
|---|---|
| In Arrgo but not Jellyfin | Trigger targeted Jellyfin refresh for that show |
| File missing from disk | Remove orphaned DB entry from Arrgo (already done by `PurgeMissingShows`) |
| In Jellyfin but not Arrgo | Trigger Arrgo scan for that show's directory |

### Scheduling

Could run as:
- **On-demand** via admin UI button
- **Post-scan hook** — run after every library scan to catch issues immediately
- **Periodic** — add to the hourly `StartIncomingScanner` loop (probably overkill)

On-demand is the simplest starting point.

## Considerations

- **Performance**: Fetching all episodes from Jellyfin for a large library could be slow. Consider paginating with `StartIndex` and `Limit` params, or processing show-by-show.
- **Specials/Season 0**: Jellyfin and TVDB sometimes disagree on where specials live. May need fuzzy matching for season 0 episodes.
- **Multi-episode files**: A single file containing S01E01-E02 will show as one file in Arrgo but two episodes in Jellyfin. Handle this by checking if adjacent episodes share the same path.
- **Depends on**: The [targeted library scans](targeted-library-scans.md) plan for the auto-fix "rescan in Jellyfin" action.
