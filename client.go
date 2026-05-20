package furcdn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultBaseURL = "https://www.furcdn.us"

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

type Domain struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type DomainsResponse struct {
	Domains []Domain `json:"domains"`
}

type PurgeResponse struct {
	OK      bool `json:"ok"`
	Total   int  `json:"total"`
	Success int  `json:"success"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("furcdn: %d %s", e.StatusCode, e.Message)
}

// New 建立新的 FurCDN API client
func New(apiKey string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// ListDomains 列出當前 API key 擁有者的所有域名
func (c *Client) ListDomains(ctx context.Context) ([]Domain, error) {
	var resp DomainsResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/domains", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Domains, nil
}

// PurgeCache 刷新指定域名所有節點的 L1+L2 快取
func (c *Client) PurgeCache(ctx context.Context, domainID int64) (*PurgeResponse, error) {
	path := fmt.Sprintf("/api/v1/domains/%d/purge", domainID)
	var resp PurgeResponse
	if err := c.do(ctx, http.MethodPost, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadSSL 為指定域名上傳 PEM 格式憑證和私鑰，會關閉自動續約
func (c *Client) UploadSSL(ctx context.Context, domainID int64, cert, key string) error {
	path := fmt.Sprintf("/api/v1/domains/%d/ssl", domainID)
	body := map[string]string{"cert": cert, "key": key}
	var resp OKResponse
	return c.do(ctx, http.MethodPost, path, body, &resp)
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		return &APIError{StatusCode: res.StatusCode, Message: e.Error}
	}

	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}
