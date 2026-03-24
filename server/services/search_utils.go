package services

import "strings"

// ExpandSearchQuery returns a slice of search query variants.
// If the query contains "&", it returns both the original query and a version with "and" replacing "&".
// This helps improve search results by matching titles that use either "&" or "and".
func ExpandSearchQuery(query string) []string {
	if !strings.Contains(query, "&") {
		return []string{query}
	}

	// Return both the original query and a version with "and" replacing "&"
	variants := []string{query}
	variants = append(variants, strings.ReplaceAll(query, "&", "and"))
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
