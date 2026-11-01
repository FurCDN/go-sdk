package furcdn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultBaseURL = "https://www.furcdn.us"

const (
	// maxPurgeEntries 是選擇性清除（urls + prefixes + patterns）的總筆數上限
	maxPurgeEntries = 100
	// maxPurgeEntryLength 是單一清除項目的長度上限
	maxPurgeEntryLength = 2048
)

// ErrInvalidPurgeOptions 表示 PurgeOptions 未通過本地驗證，請求尚未送出。
// 可用 errors.Is(err, furcdn.ErrInvalidPurgeOptions) 判斷。
var ErrInvalidPurgeOptions = errors.New("furcdn: 無效的清除參數")

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
	OK   bool   `json:"ok"`
	Mode string `json:"mode"`
	// Total 是正在服務該域名的節點數，Success 是已確認的節點數。
	Total   int `json:"total"`
	Success int `json:"success"`
	// Removed 僅在選擇性清除（Mode == "selective"）時有值，
	// 代表各節點刪除的快取項目總數（同一 URL 快取在 3 個節點就算 3 筆）。
	Removed int `json:"removed"`
}

// PurgeOptions 描述要清除的範圍。零值不是有效請求：
// 至少要設定 All，或 urls / prefixes / patterns 其中之一。
//
//   - All：清除整個域名的快取（等同舊版 PurgeCache）。
//   - URLs：精確 URL 清除。可為絕對 URL（host 必須等於域名）或以 "/" 開頭的路徑。
//     query string 屬於比對條件，除非該域名開啟了 ignoreQueryString。
//   - Prefixes：以 URL 路徑做字串前綴比對（忽略 query），例如 "/static/" 會清掉
//     "/static/a.js" 與 "/static/x/y.css"。
//   - Patterns：以 URL 路徑做 glob 比對，* 可匹配任意字元（含 "/"），必須以 "/" 開頭。
//
// All 不可與其他三者同時使用；urls / prefixes / patterns 可混用（聯集）。
type PurgeOptions struct {
	All      bool     `json:"all,omitempty"`
	URLs     []string `json:"urls,omitempty"`
	Prefixes []string `json:"prefixes,omitempty"`
	Patterns []string `json:"patterns,omitempty"`
}

// validate 在送出請求前檢查參數，避免無謂的 API 呼叫與 400 回應。
func (o PurgeOptions) validate() error {
	selectors := len(o.URLs) + len(o.Prefixes) + len(o.Patterns)

	if o.All && selectors > 0 {
		return fmt.Errorf("%w: all 不可與 urls/prefixes/patterns 同時使用", ErrInvalidPurgeOptions)
	}
	if !o.All && selectors == 0 {
		return fmt.Errorf("%w: 必須設定 all，或至少一個 urls/prefixes/patterns 項目", ErrInvalidPurgeOptions)
	}
	if selectors > maxPurgeEntries {
		return fmt.Errorf("%w: 選擇性清除共 %d 筆，超過上限 %d 筆", ErrInvalidPurgeOptions, selectors, maxPurgeEntries)
	}

	for _, group := range []struct {
		name   string
		values []string
	}{
		{"urls", o.URLs},
		{"prefixes", o.Prefixes},
		{"patterns", o.Patterns},
	} {
		for i, v := range group.values {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("%w: %s[%d] 不可為空字串", ErrInvalidPurgeOptions, group.name, i)
			}
			if len(v) > maxPurgeEntryLength {
				return fmt.Errorf("%w: %s[%d] 長度 %d 超過上限 %d", ErrInvalidPurgeOptions, group.name, i, len(v), maxPurgeEntryLength)
			}
		}
	}
	return nil
}

type OKResponse struct {
	OK bool `json:"ok"`
}

type OriginIPsResponse struct {
	IPs   []string `json:"ips"`
	Count int      `json:"count"`
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

// PurgeCacheWith 依 opts 選擇性刷新指定域名的快取。
//
// 只想清整個域名時請直接用 PurgeCache；要精確清掉某些 URL、路徑前綴或 glob 樣式時用本方法。
// 參數會在本地先驗證，不合法時回傳可被 errors.Is 判定的 ErrInvalidPurgeOptions，
// 且不會送出任何 HTTP 請求。
//
// 注意：快取規則（TTL／不快取）只在節點回源時套用，改規則不會動到已快取的內容；
// 要讓規則變更立即生效，請清除對應的快取。
func (c *Client) PurgeCacheWith(ctx context.Context, domainID int64, opts PurgeOptions) (*PurgeResponse, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v1/domains/%d/purge", domainID)
	var resp PurgeResponse
	if err := c.do(ctx, http.MethodPost, path, opts, &resp); err != nil {
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

// OriginIPs 取得目前全部節點的回源 IP，供源站防火牆白名單使用。
//
// 這是公開端點，不需要 API key（SDK 仍會帶上 Authorization，端點會忽略）。
// 清單「不是固定的」：節點隨時新增 / 下線 / 換 IP。請勿寫死，建議每 5~10 分鐘
// 重新取得後同步防火牆規則。更可靠的回源鑑別方式是自定義回源 header。
func (c *Client) OriginIPs(ctx context.Context) ([]string, error) {
	var resp OriginIPsResponse
	if err := c.do(ctx, http.MethodGet, "/api/public/origin-ips?format=json", nil, &resp); err != nil {
		return nil, err
	}
	return resp.IPs, nil
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
