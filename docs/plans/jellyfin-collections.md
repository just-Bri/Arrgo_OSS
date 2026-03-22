# Jellyfin Auto-Collections

## Problem

Related media (franchise series, sequels, shared universe) sits scattered across the library in Jellyfin with no grouping. Users have to manually create collections in Jellyfin, and those collections break when items get renamed or re-imported by Arrgo.

## Goal

Automatically create and maintain Jellyfin collections based on franchise/series relationships, driven by metadata Arrgo already has.

## Jellyfin API

### Create a Collection
```
POST /Collections?Name={name}&Ids={item1Id},{item2Id},...
```

Returns the new collection's ID. Collections are virtual groupings — items aren't moved.

### Add Items to Existing Collection
```
POST /Collections/{collectionId}/Items?Ids={itemId1},{itemId2}
```

### Remove Items from Collection
```
DELETE /Collections/{collectionId}/Items?Ids={itemId1},{itemId2}
```

### List Collections
```
GET /Items?IncludeItemTypes=BoxSet&Recursive=true
```

### Get Collection Items
```
GET /Items?ParentId={collectionId}
```

### Set Collection Image
```
POST /Items/{collectionId}/Images/Primary
Content-Type: image/jpeg
Body: <image bytes>
```

## Collection Sources

### 1. TMDB Collections (Movies)

TMDB groups movies into official collections (e.g., "The Dark Knight Collection", "John Wick Collection"). This data is already available in Arrgo's `raw_metadata` JSONB field for movies.

**TMDB API response includes:**
```json
{
  "belongs_to_collection": {
    "id": 263,
    "name": "The Dark Knight Collection"
  }
}
```

This is the easiest and most reliable source — no guessing.

### 2. TVDB Series Relationships (Shows)

Related shows can be grouped manually by defining collection rules. Examples:
- All Gundam series (multiple TVDB IDs)
- Monogatari series
- Star Trek franchise

These need a user-defined mapping since TVDB doesn't have a standard "franchise" field.

### 3. User-Defined Collections

Let users create custom collections via the Arrgo UI, selecting shows and movies to group together.

## Implementation

### Phase 1: TMDB Movie Collections (Automatic)

This is the lowest-hanging fruit since the data already exists.

**Database:**
```sql
CREATE TABLE collections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    source VARCHAR(50) DEFAULT 'manual',  -- 'tmdb', 'manual'
    source_id VARCHAR(100),               -- e.g., tmdb collection ID
    jellyfin_id VARCHAR(100),             -- Jellyfin collection/BoxSet ID
    poster_url TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE collection_items (
    id SERIAL PRIMARY KEY,
    collection_id INTEGER REFERENCES collections(id) ON DELETE CASCADE,
    media_type VARCHAR(20) NOT NULL,  -- 'movie' or 'show'
    movie_id INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    show_id INTEGER REFERENCES shows(id) ON DELETE CASCADE,
    jellyfin_item_id VARCHAR(100),
    UNIQUE(collection_id, movie_id),
    UNIQUE(collection_id, show_id)
);
```

**Sync function:**

```go
func SyncTMDBCollections(cfg *config.Config) error {
    // 1. Query all movies that have raw_metadata with belongs_to_collection
    // 2. Group movies by TMDB collection ID
    // 3. For each group with 2+ movies:
    //    a. Upsert into collections table
    //    b. Resolve Jellyfin item IDs for each movie
    //    c. Create or update Jellyfin collection via API
    //    d. Store jellyfin_id back to collections table
}
```

**Trigger points:**
- After movie import (check if imported movie belongs to a collection)
- Admin action to do a full collection sync
- Could also run periodically

### Phase 2: User-Defined Collections

**API endpoints:**

```
POST   /api/admin/collections              — Create collection
GET    /api/admin/collections              — List all collections
PUT    /api/admin/collections/{id}         — Update collection name
DELETE /api/admin/collections/{id}         — Delete collection
POST   /api/admin/collections/{id}/items   — Add items
DELETE /api/admin/collections/{id}/items   — Remove items
POST   /api/admin/collections/sync         — Sync all to Jellyfin
```

**UI:**
- Admin panel section showing all collections with item counts
- Create collection form with search to add shows/movies
- Drag-and-drop reordering within a collection
- "Sync to Jellyfin" button per collection and bulk

### Phase 3: Smart Collection Suggestions

Analyze the library and suggest collections based on:
- Shared genres + keywords (e.g., all "isekai" anime)
- Same original network (e.g., all HBO originals)
- Same creator/director
- Franchise keywords in titles (e.g., "Gundam", "Star Trek", "Dragon Ball")

Present suggestions in the admin UI with one-click accept.

## Sync Strategy

### Creating Collections in Jellyfin

1. Check if a Jellyfin collection with the same name already exists (via `GET /Items?IncludeItemTypes=BoxSet&SearchTerm={name}`)
2. If exists, update its items. If not, create it.
3. Store the `jellyfin_id` in Arrgo's DB for future updates.

### Keeping Collections in Sync

When items get re-imported or renamed, their Jellyfin item IDs may change. The sync function should:
1. Re-resolve Jellyfin item IDs by provider IDs (reuse logic from targeted scans plan)
2. Compare current collection membership against desired membership
3. Add/remove items as needed

### Handling Deletions

When a movie/show is removed from Arrgo:
1. Remove it from any `collection_items` entries
2. If the collection now has fewer than 2 items, optionally delete the Jellyfin collection

## Considerations

- **Depends on**: The [targeted library scans](targeted-library-scans.md) plan for Jellyfin item ID resolution (`ResolveJellyfinItemID`).
- **Naming conflicts**: Jellyfin collection names must be unique. If a user creates a collection with the same name as a TMDB collection, the TMDB sync should find and reuse it rather than creating a duplicate.
- **Mixed collections**: User-defined collections can contain both movies and shows. The Jellyfin API supports this.
- **Collection artwork**: TMDB provides collection poster URLs. Could download and set as the Jellyfin collection image automatically.
- **Ordering**: Jellyfin displays collection items in the order they were added. For movie franchises, sort by release year before adding.
