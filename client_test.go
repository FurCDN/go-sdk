package furcdn

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
		_, _ = w.Write([]byte(`{"ok":true,"total":3,"success":3}`))
	})
	defer srv.Close()

	resp, err := c.PurgeCache(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Total != 3 || resp.Success != 3 {
		t.Errorf("unexpected: %+v", resp)
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
