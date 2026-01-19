package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"Arrgo/config"
)

type QBittorrentClient struct {
	cfg    *config.Config
	client *http.Client
	mu     sync.Mutex
}

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
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (q *QBittorrentClient) AddTorrent(ctx context.Context, magnetLink string, category string, savePath string) error {
	// Ensure we're logged in (qBittorrent might need re-auth)
	if err := q.Login(ctx); err != nil {
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add torrent: status %d, body: %s", resp.StatusCode, string(body))
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
}

func (q *QBittorrentClient) GetTorrents(ctx context.Context, filter string) ([]TorrentStatus, error) {
	if err := q.Login(ctx); err != nil {
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

	for _, t := range torrents {
		if t.Hash == hash {
			return &t, nil
		}
	}

	return nil, fmt.Errorf("torrent with hash %s not found", hash)
}

func (q *QBittorrentClient) PauseTorrent(ctx context.Context, hash string) error {
	return q.batchAction(ctx, "pause", hash)
}

func (q *QBittorrentClient) ResumeTorrent(ctx context.Context, hash string) error {
	return q.batchAction(ctx, "resume", hash)
}

func (q *QBittorrentClient) DeleteTorrent(ctx context.Context, hash string, deleteFiles bool) error {
	action := "delete"
	if err := q.Login(ctx); err != nil {
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
	if err := q.Login(ctx); err != nil {
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
