package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"Arrgo/config"
)

type QBittorrentClient struct {
	cfg          *config.Config
	client       *http.Client
	mu           sync.Mutex
	lastLogin    time.Time
	sessionValid bool
}

const (
	sessionTimeout = 15 * time.Minute // qBittorrent sessions typically last longer
)

func NewQBittorrentClient(cfg *config.Config) (*QBittorrentClient, error) {
	jar, _ := cookiejar.New(nil)
	return &QBittorrentClient{
		cfg: cfg,
		client: &http.Client{
			Jar:     jar,
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (q *QBittorrentClient) Login(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if session is still valid
	if q.sessionValid && time.Since(q.lastLogin) < sessionTimeout {
		return nil
	}

	loginURL := fmt.Sprintf("%s/api/v2/auth/login", q.cfg.QBittorrentURL)
	data := url.Values{}
	data.Set("username", q.cfg.QBittorrentUser)
	data.Set("password", q.cfg.QBittorrentPass)

	req, err := http.NewRequestWithContext(ctx, "POST", loginURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := q.client.Do(req)
	if err != nil {
		q.sessionValid = false
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		q.sessionValid = false
		return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(body))
	}

	q.lastLogin = time.Now()
	q.sessionValid = true
	return nil
}

// ensureLogin ensures we're logged in, refreshing if needed
func (q *QBittorrentClient) ensureLogin(ctx context.Context) error {
	return q.Login(ctx)
}

// AddTorrent adds a torrent to qBittorrent via magnet link or URL
func (q *QBittorrentClient) AddTorrent(ctx context.Context, magnetLink string, category string, savePath string) error {
	// Ensure we're logged in (uses cached session if valid)
	if err := q.ensureLogin(ctx); err != nil {
		return fmt.Errorf("failed to login before adding torrent: %w", err)
	}

	addURL := fmt.Sprintf("%s/api/v2/torrents/add", q.cfg.QBittorrentURL)
	data := url.Values{}
	data.Set("urls", magnetLink)
	if category != "" {
		data.Set("category", category)
	}
	if savePath != "" {
		data.Set("savepath", savePath)
	}
	// Don't pause - let qBittorrent start downloading metadata immediately
	// Skip hash checking for faster start (qBittorrent will check during download)
	data.Set("skip_checking", "false")

	req, err := http.NewRequestWithContext(ctx, "POST", addURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		// Session expired, invalidate and retry once
		q.mu.Lock()
		q.sessionValid = false
		q.mu.Unlock()

		// Retry login and request
		if err := q.ensureLogin(ctx); err != nil {
			return fmt.Errorf("failed to re-login after 403: %w", err)
		}

		// Retry the request
		req, _ = http.NewRequestWithContext(ctx, "POST", addURL, strings.NewReader(data.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err = q.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add torrent: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// AddTorrentFile adds a torrent to qBittorrent via .torrent file data
func (q *QBittorrentClient) AddTorrentFile(ctx context.Context, torrentData []byte, category string, savePath string) error {
	// Ensure we're logged in (uses cached session if valid)
	if err := q.ensureLogin(ctx); err != nil {
		return fmt.Errorf("failed to login before adding torrent: %w", err)
	}

	addURL := fmt.Sprintf("%s/api/v2/torrents/add", q.cfg.QBittorrentURL)

	// Create multipart form data
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Add torrent file
	part, err := writer.CreateFormFile("torrents", "torrent.torrent")
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(torrentData); err != nil {
		return fmt.Errorf("failed to write torrent data: %w", err)
	}

	// Add category if specified
	if category != "" {
		if err := writer.WriteField("category", category); err != nil {
			return fmt.Errorf("failed to write category: %w", err)
		}
	}

	// Add save path if specified
	if savePath != "" {
		if err := writer.WriteField("savepath", savePath); err != nil {
			return fmt.Errorf("failed to write savepath: %w", err)
		}
	}

	// Skip hash checking for faster start
	if err := writer.WriteField("skip_checking", "false"); err != nil {
		return fmt.Errorf("failed to write skip_checking: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", addURL, &requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		// Session expired, invalidate and retry once
		q.mu.Lock()
		q.sessionValid = false
		q.mu.Unlock()

		// Retry login and request
		if err := q.ensureLogin(ctx); err != nil {
			return fmt.Errorf("failed to re-login after 403: %w", err)
		}

		// Recreate request body for retry
		var retryBody bytes.Buffer
		retryWriter := multipart.NewWriter(&retryBody)
		retryPart, _ := retryWriter.CreateFormFile("torrents", "torrent.torrent")
		retryPart.Write(torrentData)
		if category != "" {
			retryWriter.WriteField("category", category)
		}
		if savePath != "" {
			retryWriter.WriteField("savepath", savePath)
		}
		retryWriter.WriteField("skip_checking", "false")
		retryWriter.Close()

		req, _ = http.NewRequestWithContext(ctx, "POST", addURL, &retryBody)
		req.Header.Set("Content-Type", retryWriter.FormDataContentType())
		resp, err = q.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add torrent file: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

type TorrentStatus struct {
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	Progress      float64 `json:"progress"`
	Size          int64   `json:"size"`
	State         string  `json:"state"`
	Eta           int     `json:"eta"`
	DownloadSpeed int     `json:"dlspeed"`
	Ratio         float64 `json:"ratio"`        // Share ratio
	SeedingTime   int64   `json:"seeding_time"` // Seeding time in seconds
	SavePath      string  `json:"save_path"`    // Save path for the torrent
}

func (q *QBittorrentClient) GetTorrents(ctx context.Context, filter string) ([]TorrentStatus, error) {
	if err := q.ensureLogin(ctx); err != nil {
		return nil, fmt.Errorf("failed to login: %w", err)
	}

	listURL := fmt.Sprintf("%s/api/v2/torrents/info", q.cfg.QBittorrentURL)
	if filter != "" {
		listURL += "?filter=" + filter
	}

	req, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get torrents: status %d", resp.StatusCode)
	}

	var torrents []TorrentStatus
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, fmt.Errorf("failed to decode torrents info: %w", err)
	}

	return torrents, nil
}

func (q *QBittorrentClient) GetTorrentByHash(ctx context.Context, hash string) (*TorrentStatus, error) {
	torrents, err := q.GetTorrents(ctx, "")
	if err != nil {
		return nil, err
	}

	normalizedHash := strings.ToLower(hash)
	for _, t := range torrents {
		if strings.ToLower(t.Hash) == normalizedHash {
			return &t, nil
		}
	}

	return nil, fmt.Errorf("torrent with hash %s not found", hash)
}

// GetTorrentsDetailed gets torrents with detailed information including ratio and seeding time
func (q *QBittorrentClient) GetTorrentsDetailed(ctx context.Context, filter string) ([]TorrentStatus, error) {
	if err := q.ensureLogin(ctx); err != nil {
		return nil, fmt.Errorf("failed to login: %w", err)
	}

	// Use the properties parameter to request specific fields
	listURL := fmt.Sprintf("%s/api/v2/torrents/info", q.cfg.QBittorrentURL)
	if filter != "" {
		listURL += "?filter=" + filter
	}

	req, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get torrents: status %d", resp.StatusCode)
	}

	var torrents []TorrentStatus
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, fmt.Errorf("failed to decode torrents info: %w", err)
	}

	return torrents, nil
}

func (q *QBittorrentClient) PauseTorrent(ctx context.Context, hash string) error {
	return q.batchAction(ctx, "pause", hash)
}

func (q *QBittorrentClient) ResumeTorrent(ctx context.Context, hash string) error {
	return q.batchAction(ctx, "resume", hash)
}

// ReannounceTorrent forces qBittorrent to reannounce to all trackers
// This can help with stuck metadata downloads
func (q *QBittorrentClient) ReannounceTorrent(ctx context.Context, hash string) error {
	if err := q.ensureLogin(ctx); err != nil {
		return err
	}

	reannounceURL := fmt.Sprintf("%s/api/v2/torrents/reannounce", q.cfg.QBittorrentURL)
	data := url.Values{}
	data.Set("hashes", hash)

	req, err := http.NewRequestWithContext(ctx, "POST", reannounceURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to reannounce torrent: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (q *QBittorrentClient) DeleteTorrent(ctx context.Context, hash string, deleteFiles bool) error {
	action := "delete"
	if err := q.ensureLogin(ctx); err != nil {
		return err
	}

	deleteURL := fmt.Sprintf("%s/api/v2/torrents/%s", q.cfg.QBittorrentURL, action)
	data := url.Values{}
	data.Set("hashes", hash)
	data.Set("deleteFiles", fmt.Sprintf("%v", deleteFiles))

	req, err := http.NewRequestWithContext(ctx, "POST", deleteURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to %s torrent: status %d", action, resp.StatusCode)
	}

	return nil
}

func (q *QBittorrentClient) batchAction(ctx context.Context, action string, hash string) error {
	if err := q.ensureLogin(ctx); err != nil {
		return err
	}

	actionURL := fmt.Sprintf("%s/api/v2/torrents/%s", q.cfg.QBittorrentURL, action)
	data := url.Values{}
	data.Set("hashes", hash)

	req, err := http.NewRequestWithContext(ctx, "POST", actionURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to %s torrent: status %d", action, resp.StatusCode)
	}

	return nil
}

type TorrentFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func (q *QBittorrentClient) GetTorrentFiles(ctx context.Context, hash string) ([]TorrentFile, error) {
	if err := q.ensureLogin(ctx); err != nil {
		return nil, err
	}

	filesURL := fmt.Sprintf("%s/api/v2/torrents/files?hash=%s", q.cfg.QBittorrentURL, hash)
	req, err := http.NewRequestWithContext(ctx, "GET", filesURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get torrent files: status %d", resp.StatusCode)
	}

	var files []TorrentFile
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode torrent files: %w", err)
	}

	return files, nil
}
