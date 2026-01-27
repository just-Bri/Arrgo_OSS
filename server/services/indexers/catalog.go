package indexers

// IndexerCatalogEntry represents an available indexer in the catalog
type IndexerCatalogEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"` // "movies", "tv", "anime", "general", "games"
	ScraperType string   `json:"scraper_type"` // "yts", "nyaa", "1337x", "torrentgalaxy", "solidtorrents", "generic"
	Status      string   `json:"status"`       // "available", "coming_soon", "requires_config"
	Languages   []string `json:"languages"`
	URL         string   `json:"url,omitempty"`
	Icon        string   `json:"icon,omitempty"`
	Config      string   `json:"config,omitempty"` // JSON config for generic scrapers
}

// GetIndexerCatalog returns the full catalog of available indexers
func GetIndexerCatalog() []IndexerCatalogEntry {
	return []IndexerCatalogEntry{
		// Currently Implemented (Available)
		{
			ID:          "yts",
			Name:        "YTS",
			Description: "High-quality movie torrents with small file sizes",
			Category:    "movies",
			ScraperType: "yts",
			Status:      "available",
			Languages:   []string{"en"},
		},
		{
			ID:          "nyaa",
			Name:        "Nyaa",
			Description: "Anime and Japanese media torrents",
			Category:    "anime",
			ScraperType: "nyaa",
			Status:      "available",
			Languages:   []string{"en", "ja"},
		},
		{
			ID:          "1337x",
			Name:        "1337x",
			Description: "General torrent indexer with movies, TV shows, games, and software",
			Category:    "general",
			ScraperType: "1337x",
			Status:      "available",
			Languages:   []string{"en"},
		},
		{
			ID:          "torrentgalaxy",
			Name:        "TorrentGalaxy",
			Description: "General torrent indexer with movies, TV shows, and more",
			Category:    "general",
			ScraperType: "torrentgalaxy",
			Status:      "available",
			Languages:   []string{"en"},
		},
		{
			ID:          "solidtorrents",
			Name:        "SolidTorrents",
			Description: "General torrent search engine aggregating multiple sources",
			Category:    "general",
			ScraperType: "solidtorrents",
			Status:      "available",
			Languages:   []string{"en"},
		},

		// Popular General Indexers
		{
			ID:          "thepiratebay",
			Name:        "The Pirate Bay",
			Description: "One of the oldest and most popular torrent sites",
			Category:    "general",
			ScraperType: "generic",
			Status:      "available",
			Languages:   []string{"en"},
			Config: `{
				"base_url": "https://thepiratebay.org",
				"search_url_pattern": "/search/{query}/0/99/0",
				"use_cloudflare_bypass": false,
				"selectors": {
					"result_container": "tr",
					"title": "a",
					"size": "td",
					"seeds": "td",
					"peers": "td",
					"link": "a"
				},
				"magnet_extraction": {
					"type": "magnet",
					"link_selector": "a[href^='magnet:']",
					"extract_from_page": false
				}
			}`,
		},
		{
			ID:          "rarbg",
			Name:        "RARBG",
			Description: "High-quality movie and TV releases (archived)",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "rutracker",
			Name:        "RuTracker",
			Description: "Large Russian torrent tracker with diverse content",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"ru", "en"},
		},
		{
			ID:          "limetorrents",
			Name:        "LimeTorrents",
			Description: "General torrent indexer",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "torlock",
			Name:        "Torlock",
			Description: "Verified torrent indexer",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "magnetdl",
			Name:        "MagnetDL",
			Description: "Magnet link focused torrent indexer",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "zooqle",
			Name:        "Zooqle",
			Description: "General torrent search engine",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "torrentz2",
			Name:        "Torrentz2",
			Description: "Meta-search engine for torrents",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "eztv",
			Name:        "EZTV",
			Description: "TV show torrents",
			Category:    "tv",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "kickasstorrents",
			Name:        "KickassTorrents",
			Description: "General torrent indexer (mirrors)",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "torrentdownloads",
			Name:        "TorrentDownloads",
			Description: "General torrent indexer",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "yourbittorrent",
			Name:        "YourBittorrent",
			Description: "General torrent indexer",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "btsow",
			Name:        "BTSOW",
			Description: "General torrent search engine",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en", "zh"},
		},
		{
			ID:          "glodls",
			Name:        "GloDLS",
			Description: "General torrent indexer",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "idope",
			Name:        "iDope",
			Description: "Torrent search engine",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "monova",
			Name:        "Monova",
			Description: "General torrent indexer",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "torrentfunk",
			Name:        "TorrentFunk",
			Description: "General torrent indexer",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "torrentproject",
			Name:        "TorrentProject",
			Description: "Meta-search engine for torrents",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "btdig",
			Name:        "BTDigg",
			Description: "DHT search engine",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "torrentsme",
			Name:        "Torrents.me",
			Description: "General torrent indexer",
			Category:    "general",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},

		// Movie-Specific
		{
			ID:          "yify",
			Name:        "YIFY",
			Description: "Movie torrents (archived, use YTS)",
			Category:    "movies",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},

		// Anime-Specific
		{
			ID:          "tokyotosho",
			Name:        "Tokyo Toshokan",
			Description: "Anime and Japanese media indexer",
			Category:    "anime",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en", "ja"},
		},
		{
			ID:          "anidex",
			Name:        "AniDex",
			Description: "Anime torrent indexer",
			Category:    "anime",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},

		// Game-Specific
		{
			ID:          "fitgirl",
			Name:        "FitGirl Repacks",
			Description: "Game repacks and releases",
			Category:    "games",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "skidrow",
			Name:        "Skidrow & Reloaded",
			Description: "Game releases",
			Category:    "games",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},
		{
			ID:          "igg",
			Name:        "IGGGAMES",
			Description: "Game torrents",
			Category:    "games",
			ScraperType: "generic",
			Status:      "coming_soon",
			Languages:   []string{"en"},
		},

	}
}

// GetIndexerCatalogByCategory returns catalog entries filtered by category
func GetIndexerCatalogByCategory(category string) []IndexerCatalogEntry {
	all := GetIndexerCatalog()
	if category == "" || category == "all" {
		return all
	}

	filtered := make([]IndexerCatalogEntry, 0)
	for _, entry := range all {
		if entry.Category == category {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// GetIndexerCatalogByStatus returns catalog entries filtered by status
func GetIndexerCatalogByStatus(status string) []IndexerCatalogEntry {
	all := GetIndexerCatalog()
	if status == "" || status == "all" {
		return all
	}

	filtered := make([]IndexerCatalogEntry, 0)
	for _, entry := range all {
		if entry.Status == status {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// FindCatalogEntry finds a catalog entry by ID
func FindCatalogEntry(id string) *IndexerCatalogEntry {
	catalog := GetIndexerCatalog()
	for i := range catalog {
		if catalog[i].ID == id {
			return &catalog[i]
		}
	}
	return nil
}
