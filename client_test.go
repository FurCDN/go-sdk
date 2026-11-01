package furcdn

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func newTestClient(handler http.HandlerFunc) (*Client, *httptest.Server) {
	srv := httptest.NewServer(handler)
	c := New("fck_test_secret")
	c.BaseURL = srv.URL
	return c, srv
}

func TestNew(t *testing.T) {
	c := New("fck_a1b2c3d4_xxxx")
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("expected %s, got %s", DefaultBaseURL, c.BaseURL)
	}
	if c.APIKey != "fck_a1b2c3d4_xxxx" {
		t.Errorf("api key not set")
	}
}

func TestListDomains(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/domains" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fck_test_secret" {
			t.Errorf("auth header = %s", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"domains":[{"id":1,"name":"example.com","enabled":true}]}`))
	})
	defer srv.Close()

	domains, err := c.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].Name != "example.com" {
		t.Errorf("unexpected: %+v", domains)
	}
}

func TestPurgeCache(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/domains/42/purge" || r.Method != http.MethodPost {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		// 舊行為：整域清除不帶 body
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("body = %q, want empty", body)
		}
		if ct := r.Header.Get("Content-Type"); ct != "" {
			t.Errorf("content-type = %q, want empty", ct)
		}
		_, _ = w.Write([]byte(`{"ok":true,"mode":"all","total":3,"success":3}`))
	})
	defer srv.Close()

	resp, err := c.PurgeCache(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Total != 3 || resp.Success != 3 {
		t.Errorf("unexpected: %+v", resp)
	}
	if resp.Mode != "all" || resp.Removed != 0 {
		t.Errorf("unexpected: %+v", resp)
	}
}

func TestPurgeCacheWithBody(t *testing.T) {
	tests := []struct {
		name string
		opts PurgeOptions
		want string
	}{
		{
			name: "all",
			opts: PurgeOptions{All: true},
			want: `{"all":true}`,
		},
		{
			name: "urls",
			opts: PurgeOptions{URLs: []string{"https://example.com/a.css?v=1", "/b.js"}},
			want: `{"urls":["https://example.com/a.css?v=1","/b.js"]}`,
		},
		{
			name: "prefixes",
			opts: PurgeOptions{Prefixes: []string{"/static/", "https://example.com/img/"}},
			want: `{"prefixes":["/static/","https://example.com/img/"]}`,
		},
		{
			name: "patterns",
			opts: PurgeOptions{Patterns: []string{"/assets/*.js"}},
			want: `{"patterns":["/assets/*.js"]}`,
		},
		{
			name: "combined selectors",
			opts: PurgeOptions{
				URLs:     []string{"/a.css"},
				Prefixes: []string{"/static/"},
				Patterns: []string{"/img/*"},
			},
			want: `{"urls":["/a.css"],"prefixes":["/static/"],"patterns":["/img/*"]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/domains/42/purge" || r.Method != http.MethodPost {
					t.Errorf("got %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer fck_test_secret" {
					t.Errorf("auth header = %s", r.Header.Get("Authorization"))
				}
				if ct := r.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("content-type = %s", ct)
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != tc.want {
					t.Errorf("body = %s, want %s", body, tc.want)
				}
				_, _ = w.Write([]byte(`{"ok":true,"mode":"selective","total":3,"success":3,"removed":12}`))
			})
			defer srv.Close()

			resp, err := c.PurgeCacheWith(context.Background(), 42, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !resp.OK || resp.Mode != "selective" || resp.Total != 3 || resp.Success != 3 || resp.Removed != 12 {
				t.Errorf("unexpected: %+v", resp)
			}
		})
	}
}

func TestPurgeCacheWithLimits(t *testing.T) {
	// 剛好 100 筆、單筆剛好 2048 字元都應該放行
	full := make([]string, 100)
	for i := range full {
		full[i] = "/item/" + strconv.Itoa(i)
	}
	entry := "/" + strings.Repeat("a", maxPurgeEntryLength-1)

	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"mode":"selective","total":1,"success":1,"removed":1}`))
	})
	defer srv.Close()

	if _, err := c.PurgeCacheWith(context.Background(), 42, PurgeOptions{URLs: full}); err != nil {
		t.Fatalf("100 entries should be accepted: %v", err)
	}
	if _, err := c.PurgeCacheWith(context.Background(), 42, PurgeOptions{URLs: []string{entry}}); err != nil {
		t.Fatalf("2048-char entry should be accepted: %v", err)
	}
}

func TestPurgeCacheWithValidation(t *testing.T) {
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = "/x/" + strconv.Itoa(i)
	}
	tooLong := "/" + strings.Repeat("a", maxPurgeEntryLength)

	tests := []struct {
		name string
		opts PurgeOptions
		want string
	}{
		{"empty options", PurgeOptions{}, "必須設定 all"},
		{"all with selectors", PurgeOptions{All: true, URLs: []string{"/a"}}, "all 不可與"},
		{"all with prefix", PurgeOptions{All: true, Prefixes: []string{"/a/"}}, "all 不可與"},
		{"all with pattern", PurgeOptions{All: true, Patterns: []string{"/a/*"}}, "all 不可與"},
		{"too many entries", PurgeOptions{URLs: tooMany}, "超過上限 100 筆"},
		{"too many mixed entries", PurgeOptions{URLs: tooMany[:50], Prefixes: tooMany[:51]}, "超過上限 100 筆"},
		{"empty url entry", PurgeOptions{URLs: []string{""}}, "urls[0] 不可為空字串"},
		{"whitespace prefix entry", PurgeOptions{Prefixes: []string{"/ok/", "   "}}, "prefixes[1] 不可為空字串"},
		{"empty pattern entry", PurgeOptions{Patterns: []string{""}}, "patterns[0] 不可為空字串"},
		{"entry too long", PurgeOptions{URLs: []string{tooLong}}, "超過上限 2048"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
				requests++
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				_, _ = w.Write([]byte(`{"ok":true}`))
			})
			defer srv.Close()

			resp, err := c.PurgeCacheWith(context.Background(), 42, tc.opts)
			if err == nil {
				t.Fatalf("expected error, got resp %+v", resp)
			}
			if resp != nil {
				t.Errorf("resp = %+v, want nil", resp)
			}
			if !errors.Is(err, ErrInvalidPurgeOptions) {
				t.Errorf("err = %v, want ErrInvalidPurgeOptions", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want substring %q", err.Error(), tc.want)
			}
			if requests != 0 {
				t.Errorf("requests = %d, want 0", requests)
			}
		})
	}
}

func TestPurgeCacheWithAPIError(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"url host 與域名不符"}`))
	})
	defer srv.Close()

	_, err := c.PurgeCacheWith(context.Background(), 42, PurgeOptions{URLs: []string{"https://other.com/a.css"}})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 || !strings.Contains(apiErr.Message, "host") {
		t.Errorf("unexpected: %+v", apiErr)
	}
}

func TestUploadSSL(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/domains/7/ssl" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %s", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]string
		_ = json.Unmarshal(body, &m)
		if m["cert"] != "CERT" || m["key"] != "KEY" {
			t.Errorf("body = %s", body)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	defer srv.Close()

	if err := c.UploadSSL(context.Background(), 7, "CERT", "KEY"); err != nil {
		t.Fatal(err)
	}
}

func TestOriginIPs(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/origin-ips" || r.Method != http.MethodGet {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("format = %s", r.URL.Query().Get("format"))
		}
		_, _ = w.Write([]byte(`{"ips":["1.2.3.4","5.6.7.8"],"count":2}`))
	})
	defer srv.Close()

	ips, err := c.OriginIPs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 2 || ips[0] != "1.2.3.4" || ips[1] != "5.6.7.8" {
		t.Errorf("unexpected: %+v", ips)
	}
}

func TestAPIError(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"未授權"}`))
	})
	defer srv.Close()

	_, err := c.ListDomains(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 401 || !strings.Contains(apiErr.Message, "未授權") {
		t.Errorf("unexpected: %+v", apiErr)
	}
}
