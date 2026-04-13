package services

import "strings"

// ExpandSearchQuery returns a slice of search query variants to maximize indexer recall.
// It handles common differences between canonical titles and scene release naming:
//   - "&" vs "and"
//   - Colons stripped (scene releases drop "Avatar: The Last..." → "Avatar The Last...")
//   - Apostrophes stripped ("It's Always Sunny" → "Its Always Sunny")
func ExpandSearchQuery(query string) []string {
	variants := []string{query}

	add := func(v string) {
		v = strings.TrimSpace(strings.Join(strings.Fields(v), " ")) // normalize whitespace
		for _, existing := range variants {
			if existing == v {
				return
			}
		}
		variants = append(variants, v)
	}

	// "&" → "and"
	if strings.Contains(query, "&") {
		add(strings.ReplaceAll(query, "&", "and"))
	}

	// Strip colons and apostrophes (scene naming convention)
	stripped := query
	stripped = strings.ReplaceAll(stripped, ":", "")
	stripped = strings.ReplaceAll(stripped, "'", "")  // straight apostrophe
	stripped = strings.ReplaceAll(stripped, "\u2019", "") // curly apostrophe
	add(stripped)

	return variants
}

// GetSearchVariantsForDB returns SQL-friendly search patterns for database queries.
// For queries with "&", it returns patterns that match both "&" and "and".
// This is used with ILIKE queries to match titles regardless of whether they use "&" or "and".
func GetSearchVariantsForDB(query string) []string {
	variants := ExpandSearchQuery(query)
	
	// Convert to SQL ILIKE patterns (with % wildcards)
	result := make([]string, 0, len(variants))
	for _, variant := range variants {
		result = append(result, "%"+variant+"%")
	}
	return result
}
