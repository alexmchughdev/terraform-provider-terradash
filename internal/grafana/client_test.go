package grafana

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newClient(t *testing.T, h http.Handler, cfg Config) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	if cfg.URL == "" {
		cfg.URL = srv.URL
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c.backoffBase, c.backoffCap, c.retryAfterUnit = time.Millisecond, 5*time.Millisecond, 10*time.Millisecond
	return c, srv
}

func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}
}

func TestNewValidation(t *testing.T) {
	for _, u := range []string{
		"", "localhost:3000", "ftp://host", "http://", "http://u:p@host",
		"http://host?x=1", "http://host#frag", "/relative",
	} {
		if _, err := New(Config{URL: u}); err == nil {
			t.Errorf("New(%q) succeeded, want error", u)
		}
	}
	c, err := New(Config{URL: "https://host/grafana/"})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "https://host/grafana" {
		t.Errorf("BaseURL = %q", c.BaseURL())
	}
}

func TestNewBadCA(t *testing.T) {
	if _, err := New(Config{URL: "https://host", CACertPEM: []byte("junk")}); err == nil {
		t.Error("want error for invalid CA bundle")
	}
}

func TestAuthAndHeaders(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		wantAuth string
	}{
		{"anonymous empty", Config{}, ""},
		{"anonymous keyword", Config{Auth: "anonymous"}, ""},
		{"bearer", Config{Auth: "glsa_token"}, "Bearer glsa_token"},
		{"basic", Config{Auth: "admin:secret"}, "Basic YWRtaW46c2VjcmV0"},
		{"custom cannot override auth", Config{Auth: "tok", Headers: map[string]string{"Authorization": "evil"}}, "Bearer tok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got http.Header
			tt.cfg.OrgID = 7
			tt.cfg.UserAgent = "tg/1"
			if tt.cfg.Headers == nil {
				tt.cfg.Headers = map[string]string{}
			}
			tt.cfg.Headers["X-Custom"] = "yes"
			c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
				io.WriteString(w, `{}`)
			}), tt.cfg)
			if _, err := c.GetDashboard(context.Background(), "abc"); err != nil {
				t.Fatal(err)
			}
			if got.Get("Authorization") != tt.wantAuth {
				t.Errorf("Authorization = %q, want %q", got.Get("Authorization"), tt.wantAuth)
			}
			for k, v := range map[string]string{
				"X-Grafana-Org-Id": "7", "X-Custom": "yes", "User-Agent": "tg/1", "Accept": "application/json",
			} {
				if got.Get(k) != v {
					t.Errorf("%s = %q, want %q", k, got.Get(k), v)
				}
			}
		})
	}
}

func TestNoOrgHeaderByDefault(t *testing.T) {
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["X-Grafana-Org-Id"]; ok {
			t.Error("unexpected org header")
		}
		io.WriteString(w, `{}`)
	}), Config{})
	if _, err := c.GetDashboard(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
}

func TestPaths(t *testing.T) {
	tests := []struct {
		name, base, uid, wantEscaped string
	}{
		{"plain", "", "abc", "/api/dashboards/uid/abc"},
		{"base path", "/grafana", "abc", "/grafana/api/dashboards/uid/abc"},
		{"trailing slash", "/grafana/", "abc", "/grafana/api/dashboards/uid/abc"},
		{"escaped uid", "", "a/b c?d", "/api/dashboards/uid/a%2Fb%20c%3Fd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.EscapedPath()
				io.WriteString(w, `{}`)
			}))
			defer srv.Close()
			c, err := New(Config{URL: srv.URL + tt.base})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.GetDashboard(context.Background(), tt.uid); err != nil {
				t.Fatal(err)
			}
			if got != tt.wantEscaped {
				t.Errorf("path = %q, want %q", got, tt.wantEscaped)
			}
		})
	}
}

func TestGetDashboard(t *testing.T) {
	c, _ := newClient(t, jsonHandler(`{"dashboard":{"uid":"u1","title":"T","version":3},"meta":{"url":"/d/u1/t","folderUid":"f1","version":3,"provisioned":true}}`), Config{})
	d, err := c.GetDashboard(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	want := Meta{URL: "/d/u1/t", FolderUID: "f1", Version: 3, Provisioned: true}
	if d.Meta != want || d.Model["title"] != "T" {
		t.Errorf("got %+v", d)
	}
}

func TestNumbersPreserved(t *testing.T) {
	const body = `{"dashboard":{"a":0.1,"b":12345678901234567890,"c":[1.0,2.50]},"meta":{}}`
	c, _ := newClient(t, jsonHandler(body), Config{})
	d, err := c.GetDashboard(context.Background(), "u")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Model["a"].(json.Number); !ok {
		t.Fatalf("a is %T, want json.Number", d.Model["a"])
	}
	out, err := json.Marshal(d.Model)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"a":0.1,"b":12345678901234567890,"c":[1.0,2.50]}`; string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
}

func TestSaveDashboard(t *testing.T) {
	var gotBody string
	var gotMethod, gotCT string
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotMethod, gotCT = string(b), r.Method, r.Header.Get("Content-Type")
		io.WriteString(w, `{"uid":"u1","url":"/d/u1","version":2}`)
	}), Config{})
	resp, err := c.SaveDashboard(context.Background(), SaveRequest{
		Dashboard: map[string]any{"title": "T", "n": json.Number("12345678901234567890")},
		FolderUID: "f1",
		Overwrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if *resp != (SaveResponse{UID: "u1", URL: "/d/u1", Version: 2}) {
		t.Errorf("resp = %+v", resp)
	}
	if gotMethod != http.MethodPost || gotCT != "application/json" {
		t.Errorf("method=%s content-type=%s", gotMethod, gotCT)
	}
	if want := `{"dashboard":{"n":12345678901234567890,"title":"T"},"folderUid":"f1","overwrite":true}`; gotBody != want {
		t.Errorf("body = %s", gotBody)
	}
}

func TestDeleteDashboard(t *testing.T) {
	tests := []struct {
		status  int
		wantErr bool
	}{
		{http.StatusOK, false},
		{http.StatusNotFound, false},
		{http.StatusForbidden, true},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			var method string
			c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method = r.Method
				w.WriteHeader(tt.status)
				io.WriteString(w, `{"message":"x"}`)
			}), Config{})
			err := c.DeleteDashboard(context.Background(), "u")
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if method != http.MethodDelete {
				t.Errorf("method = %s", method)
			}
		})
	}
}

func TestNotFound(t *testing.T) {
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Dashboard not found"}`)
	}), Config{})
	_, err := c.GetDashboard(context.Background(), "u")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.StatusCode != 404 {
		t.Errorf("want APIError 404, got %v", err)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"json message", `{"message":"bad things","status":"error"}`, "grafana: forbidden (403): bad things"},
		{"plain body", "  nope \n", "grafana: forbidden (403): nope"},
		{"truncated", strings.Repeat("x", 2000), "grafana: forbidden (403): " + strings.Repeat("x", 512) + "…"},
		{"truncated mid rune", strings.Repeat("€", 500), "grafana: forbidden (403): " + strings.Repeat("€", 170) + "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, tt.body)
			}), Config{Auth: "admin:hunter2"})
			_, err := c.GetDashboard(context.Background(), "u")
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatalf("err = %v", err)
			}
			if ae.Error() != tt.want {
				t.Errorf("got %q, want %q", ae.Error(), tt.want)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Error("error leaks credentials")
			}
		})
	}
}

func TestRetries(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		status    int
		retries   int
		wantCalls int32
		wantErr   bool
	}{
		{"get 503 then ok", http.MethodGet, 503, 3, 3, false},
		{"get 400 not retried", http.MethodGet, 400, 3, 1, true},
		{"get no retries configured", http.MethodGet, 503, 0, 1, true},
		{"negative retries treated as zero", http.MethodGet, 503, -1, 1, true},
		{"post 500 not retried", http.MethodPost, 500, 3, 1, true},
		{"post 502 not retried", http.MethodPost, 502, 3, 1, true},
		{"post 429 retried", http.MethodPost, 429, 3, 3, false},
		{"post 503 retried", http.MethodPost, 503, 3, 3, false},
		{"delete 500 retried", http.MethodDelete, 500, 3, 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			var bodies []string
			c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				bodies = append(bodies, string(b))
				if calls.Add(1) < 3 {
					w.WriteHeader(tt.status)
					return
				}
				io.WriteString(w, `{"dashboard":{},"meta":{}}`)
			}), Config{Retries: tt.retries})
			err := callMethod(c, tt.method)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if calls.Load() != tt.wantCalls {
				t.Errorf("calls = %d, want %d", calls.Load(), tt.wantCalls)
			}
			for _, b := range bodies {
				if tt.method == http.MethodPost && b != bodies[0] {
					t.Errorf("body changed across retries: %q vs %q", b, bodies[0])
				}
			}
		})
	}
}

func callMethod(c *Client, method string) error {
	ctx := context.Background()
	switch method {
	case http.MethodPost:
		_, err := c.SaveDashboard(ctx, SaveRequest{Dashboard: map[string]any{"title": "T"}})
		return err
	case http.MethodDelete:
		return c.DeleteDashboard(ctx, "u")
	}
	_, err := c.GetDashboard(ctx, "u")
	return err
}

func TestRetryTransportError(t *testing.T) {
	var calls atomic.Int32
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		io.WriteString(w, `{}`)
	}), Config{Retries: 2})
	if _, err := c.GetDashboard(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

func TestRetryAfter(t *testing.T) {
	var calls atomic.Int32
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{}`)
	}), Config{Retries: 1})
	c.backoffBase, c.backoffCap, c.retryAfterUnit = time.Nanosecond, time.Nanosecond, 100*time.Millisecond
	start := time.Now()
	if _, err := c.GetDashboard(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("elapsed %v, want >= 200ms", elapsed)
	}
}

func TestContextCancelStopsRetries(t *testing.T) {
	var calls atomic.Int32
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}), Config{Retries: 5})
	c.backoffBase, c.backoffCap = time.Hour, time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.GetDashboard(ctx, "u")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want deadline exceeded", err)
	}
	if calls.Load() != 1 || time.Since(start) > 2*time.Second {
		t.Errorf("calls = %d, elapsed %v", calls.Load(), time.Since(start))
	}
}

func TestCrossHostRedirectRefused(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(true)
	}))
	defer other.Close()

	var calls atomic.Int32
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, other.URL+"/x", http.StatusFound)
	}), Config{Auth: "tok", Retries: 3})
	_, err := c.GetDashboard(context.Background(), "u")
	if !errors.Is(err, errRedirect) {
		t.Fatalf("err = %v, want redirect refusal", err)
	}
	if leaked.Load() {
		t.Error("request reached other host")
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, redirect refusal must not be retried", calls.Load())
	}
}

func TestSameHostRedirectFollowed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/dashboards/uid/u", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", jsonHandler(`{}`))
	c, _ := newClient(t, mux, Config{})
	if _, err := c.GetDashboard(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
}

func TestBodySizeLimit(t *testing.T) {
	c, _ := newClient(t, jsonHandler(`{"dashboard":{"title":"`+strings.Repeat("x", 200)+`"},"meta":{}}`), Config{Retries: 2})
	c.maxBodySize = 64
	_, err := c.GetDashboard(context.Background(), "u")
	if !errors.Is(err, errTooLarge) {
		t.Errorf("err = %v, want errTooLarge", err)
	}

	c, _ = newClient(t, jsonHandler(`{"dashboard":{},"meta":{}}`), Config{})
	c.maxBodySize = 64
	if _, err := c.GetDashboard(context.Background(), "u"); err != nil {
		t.Errorf("small body: %v", err)
	}
}

func TestSearchPaginationAndFolders(t *testing.T) {
	var queries []map[string][]string
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		queries = append(queries, q)
		n := 0
		switch q.Get("page") {
		case "1", "2":
			n = searchLimit
		case "3":
			n = 5
		}
		hits := make([]SearchHit, n)
		for i := range hits {
			hits[i] = SearchHit{UID: fmt.Sprintf("p%s-%d", q.Get("page"), i), Title: "t", FolderUID: "f1"}
		}
		json.NewEncoder(w).Encode(hits)
	}), Config{})

	hits, err := c.SearchDashboards(context.Background(), []string{"f1", "f2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2*searchLimit+5 {
		t.Errorf("hits = %d", len(hits))
	}
	if len(queries) != 3 {
		t.Fatalf("requests = %d, want 3", len(queries))
	}
	q := queries[0]
	if q["type"][0] != "dash-db" || q["limit"][0] != "1000" || q["page"][0] != "1" {
		t.Errorf("query = %v", q)
	}
	if f := q["folderUIDs"]; len(f) != 2 || f[0] != "f1" || f[1] != "f2" {
		t.Errorf("folderUIDs = %v", f)
	}
}

func TestSearchWithoutFolders(t *testing.T) {
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["folderUIDs"]; ok {
			t.Error("unexpected folderUIDs")
		}
		io.WriteString(w, `[{"uid":"a","title":"A","folderUid":"f"}]`)
	}), Config{})
	hits, err := c.SearchDashboards(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0] != (SearchHit{UID: "a", Title: "A", FolderUID: "f"}) {
		t.Errorf("hits = %+v", hits)
	}
}

func TestRetryAfterClamped(t *testing.T) {
	c, _ := newClient(t, jsonHandler(`{}`), Config{})
	c.retryAfterUnit, c.maxRetryAfter = time.Second, 30*time.Second
	tests := []struct {
		value  string
		want   time.Duration
		wantOK bool
	}{
		{"2", 2 * time.Second, true},
		{"30", 30 * time.Second, true},
		{"31", 30 * time.Second, true},
		{"9223372036854775807", 30 * time.Second, true},
		{"-5", 0, false},
		{"soon", 0, false},
		{"", 0, false},
		{time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat), 0, true},
		{time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), 30 * time.Second, true},
	}
	for _, tt := range tests {
		got, ok := c.retryAfter(tt.value)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("retryAfter(%q) = %v, %v; want %v, %v", tt.value, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestRetryAfterHugeValueDoesNotStall(t *testing.T) {
	var calls atomic.Int32
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "999999999999")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		io.WriteString(w, `{}`)
	}), Config{Retries: 1})
	c.maxRetryAfter = 50 * time.Millisecond
	start := time.Now()
	if _, err := c.GetDashboard(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("elapsed %v, want clamped", elapsed)
	}
}

func TestTLSVerificationFailureNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{URL: srv.URL, Retries: 3})
	if err != nil {
		t.Fatal(err)
	}
	c.backoffBase, c.backoffCap = time.Hour, time.Hour
	_, err = c.GetDashboard(context.Background(), "u")
	var verify *tls.CertificateVerificationError
	if !errors.As(err, &verify) {
		t.Fatalf("err = %v, want certificate verification error", err)
	}
	if calls.Load() != 0 {
		t.Errorf("server reached %d times", calls.Load())
	}
}

func TestSearchStopsWhenPageIgnored(t *testing.T) {
	var calls atomic.Int32
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		hits := make([]SearchHit, searchLimit)
		for i := range hits {
			hits[i] = SearchHit{UID: fmt.Sprintf("u%d", i)}
		}
		json.NewEncoder(w).Encode(hits)
	}), Config{})
	hits, err := c.SearchDashboards(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != searchLimit || calls.Load() != 2 {
		t.Errorf("hits = %d, calls = %d; want %d, 2", len(hits), calls.Load(), searchLimit)
	}
}

func TestParseBaseURL(t *testing.T) {
	tests := []struct {
		raw     string
		wantErr bool
	}{
		{"https://grafana.example.com", false},
		{"http://localhost:3000/grafana/", false},
		{"", true},
		{"localhost:3000", true},
		{"ftp://host", true},
		{"http://", true},
		{"http://u:p@host", true},
		{"http://host?x=1", true},
		{"http://host#frag", true},
		{"/relative", true},
	}
	for _, tt := range tests {
		u, err := ParseBaseURL(tt.raw)
		if (err != nil) != tt.wantErr || (err == nil && u == nil) {
			t.Errorf("ParseBaseURL(%q) = %v, %v; wantErr %v", tt.raw, u, err, tt.wantErr)
		}
	}
}

func TestSendsPlaintextCredentials(t *testing.T) {
	tests := []struct {
		url, auth string
		want      bool
	}{
		{"http://grafana.example.com", "tok", true},
		{"http://10.0.0.5:3000", "admin:pw", true},
		{"https://grafana.example.com", "tok", false},
		{"http://grafana.example.com", "", false},
		{"http://grafana.example.com", "anonymous", false},
		{"http://localhost:3000", "tok", false},
		{"http://127.0.0.1:3000", "tok", false},
		{"http://[::1]:3000", "tok", false},
		{"://bad", "tok", false},
	}
	for _, tt := range tests {
		if got := (Config{URL: tt.url, Auth: tt.auth}).SendsPlaintextCredentials(); got != tt.want {
			t.Errorf("SendsPlaintextCredentials(%q, %q) = %v, want %v", tt.url, tt.auth, got, tt.want)
		}
	}
}
