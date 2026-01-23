package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
)

type YTSResponse struct {
	Status        string `json:"status"`
	StatusMessage string `json:"status_message"`
	Data          struct {
		MovieCount int `json:"movie_count"`
		Limit      int `json:"limit"`
		PageNumber int `json:"page_number"`
		Movies     []struct {
			ID               int      `json:"id"`
			URL              string   `json:"url"`
			IMDBCode         string   `json:"imdb_code"`
			Title            string   `json:"title"`
			TitleEnglish     string   `json:"title_english"`
			TitleLong        string   `json:"title_long"`
			Slug             string   `json:"slug"`
			Year             int      `json:"year"`
			Rating           float64  `json:"rating"`
			Runtime          int      `json:"runtime"`
			Genres           []string `json:"genres"`
			Summary          string   `json:"summary"`
			DescriptionFull  string   `json:"description_full"`
			Synopsis         string   `json:"synopsis"`
			YTTrailerCode    string   `json:"yt_trailer_code"`
			Language         string   `json:"language"`
			MPARating        string   `json:"mpa_rating"`
			BackgroundImage  string   `json:"background_image"`
			SmallCoverImage  string   `json:"small_cover_image"`
			MediumCoverImage string   `json:"medium_cover_image"`
			LargeCoverImage  string   `json:"large_cover_image"`
			State            string   `json:"state"`
			Torrents         []struct {
				URL           string `json:"url"`
				Hash          string `json:"hash"`
				Quality       string `json:"quality"`
				Type          string `json:"type"`
				IsRepack      string `json:"is_repack"`
				VideoCodec    string `json:"video_codec"`
				BitDepth      string `json:"bit_depth"`
				AudioChannels string `json:"audio_channels"`
				Seeds         int    `json:"seeds"`
				Peers         int    `json:"peers"`
				Size          string `json:"size"`
				SizeBytes     int64  `json:"size_bytes"`
				DateUploaded  string `json:"date_uploaded"`
			} `json:"torrents"`
			DateUploaded string `json:"date_uploaded"`
		} `json:"movies"`
	} `json:"data"`
}

type YTSIndexer struct{}

func (y *YTSIndexer) GetName() string {
	return "YTS"
}

func (y *YTSIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	// YTS API endpoints - try primary first, then fallback
	baseURLs := []string{
		"https://yts.torrentbay.st", // Primary
		"https://yts.bz",             // Fallback
	}

	queryParam := fmt.Sprintf("query_term=%s&sort_by=seeds", url.QueryEscape(query))
	var lastErr error

	for i, baseURL := range baseURLs {
		apiURL := fmt.Sprintf("%s/api/v2/list_movies.json?%s", baseURL, queryParam)
		
		resp, err := MakeHTTPRequest(ctx, apiURL, DefaultHTTPClient)
		if err != nil {
			lastErr = err
			if i < len(baseURLs)-1 {
				slog.Debug("YTS primary endpoint failed, trying fallback", "url", baseURL, "error", err)
				continue // Try next URL
			}
			return nil, fmt.Errorf("failed to fetch results from all YTS endpoints: %w", err)
		}

		var ytsResp YTSResponse
		if err := DecodeJSONResponse(resp, &ytsResp); err != nil {
			resp.Body.Close()
			lastErr = err
			if i < len(baseURLs)-1 {
				slog.Debug("YTS primary endpoint response invalid, trying fallback", "url", baseURL, "error", err)
				continue // Try next URL
			}
			return nil, fmt.Errorf("failed to decode response from all YTS endpoints: %w", err)
		}
		resp.Body.Close()

		// Success - process results
		results := []SearchResult{}
		for _, movie := range ytsResp.Data.Movies {
			for _, torrent := range movie.Torrents {
				// Construct magnet link (YTS usually provides a hash, we can build the magnet link)
				// tr are trackers
				magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=udp://open.demonii.com:1337/announce&tr=udp://tracker.openbittorrent.com:80&tr=udp://tracker.coppersurfer.tk:6969&tr=udp://glotorrents.pw:6969/announce&tr=udp://tracker.opentrackr.org:1337/announce", 
					torrent.Hash, url.QueryEscape(movie.TitleLong))
				
				results = append(results, SearchResult{
					Title:      fmt.Sprintf("%s (%s) [%s]", movie.Title, torrent.Quality, torrent.VideoCodec),
					Size:       torrent.Size,
					Seeds:      torrent.Seeds,
					Peers:      torrent.Peers,
					MagnetLink: magnet,
					InfoHash:   torrent.Hash,
					Source:     "YTS",
					Resolution: torrent.Quality,
					Quality:    torrent.Type,
				})
			}
		}

		if i > 0 {
			slog.Info("YTS fallback endpoint succeeded", "url", baseURL, "results", len(results))
		}

		return results, nil
	}

	return nil, lastErr
}

func (y *YTSIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	// YTS only does movies
	return nil, nil
}
