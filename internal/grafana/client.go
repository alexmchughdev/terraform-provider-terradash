package grafana

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("not found")

	errRedirect = errors.New("refusing redirect to a different host")
	errTooLarge = errors.New("response body too large")
)

const (
	defaultTimeout = 60 * time.Second
	maxErrMessage  = 512
	searchLimit    = 1000
	maxSearchPages = 1000

	// DefaultRetries is a sensible Config.Retries; zero means no retries.
	DefaultRetries = 3

	defaultMaxBodySize   int64 = 64 << 20
	defaultBackoffBase         = 250 * time.Millisecond
	defaultBackoffCap          = 5 * time.Second
	defaultMaxRetryAfter       = 30 * time.Second
)

type Config struct {
	URL                string
	Auth               string
	OrgID              int64
	Headers            map[string]string
	InsecureSkipVerify bool
	CACertPEM          []byte
	Timeout            time.Duration
	Retries            int
	UserAgent          string
}

type Client struct {
	base string
	cfg  Config
	http *http.Client

	maxBodySize    int64
	backoffBase    time.Duration
	backoffCap     time.Duration
	retryAfterUnit time.Duration
	maxRetryAfter  time.Duration
}

type Dashboard struct {
	Model map[string]any `json:"dashboard"`
	Meta  Meta           `json:"meta"`
}

type Meta struct {
	URL         string `json:"url"`
	FolderUID   string `json:"folderUid"`
	Version     int64  `json:"version"`
	Provisioned bool   `json:"provisioned"`
}

type SaveRequest struct {
	Dashboard map[string]any `json:"dashboard"`
	FolderUID string         `json:"folderUid,omitempty"`
	Overwrite bool           `json:"overwrite"`
	Message   string         `json:"message,omitempty"`
}

type SaveResponse struct {
	UID     string `json:"uid"`
	URL     string `json:"url"`
	Version int64  `json:"version"`
}

type SearchHit struct {
	UID       string `json:"uid"`
	Title     string `json:"title"`
	FolderUID string `json:"folderUid"`
}

type APIError struct {
	StatusCode int
	Message    string
}

// SendsPlaintextCredentials reports whether credentials would travel over
// http to a non-loopback host.
func (c Config) SendsPlaintextCredentials() bool {
	if c.Auth == "" || c.Auth == "anonymous" {
		return false
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

func (e *APIError) Error() string {
	return fmt.Sprintf("grafana: %s (%d): %s", strings.ToLower(http.StatusText(e.StatusCode)), e.StatusCode, e.Message)
}

func New(cfg Config) (*Client, error) {
	u, err := ParseBaseURL(cfg.URL)
	if err != nil {
		return nil, err
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	cfg.Retries = max(cfg.Retries, 0)
	transport, err := newTransport(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{
		base: strings.TrimRight(u.String(), "/"),
		cfg:  cfg,
		http: &http.Client{
			Timeout:       cfg.Timeout,
			Transport:     transport,
			CheckRedirect: checkRedirect,
		},
		maxBodySize:    defaultMaxBodySize,
		backoffBase:    defaultBackoffBase,
		backoffCap:     defaultBackoffCap,
		retryAfterUnit: time.Second,
		maxRetryAfter:  defaultMaxRetryAfter,
	}, nil
}

// ParseBaseURL validates a Grafana base URL.
func ParseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("grafana: invalid url: %w", err)
	}
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return nil, errors.New("grafana: url scheme must be http or https")
	case u.Host == "":
		return nil, errors.New("grafana: url must include a host")
	case u.User != nil:
		return nil, errors.New("grafana: url must not contain credentials")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return nil, errors.New("grafana: url must not contain a query or fragment")
	}
	return u, nil
}

func newTransport(cfg Config) (*http.Transport, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec // explicit opt-in
	if len(cfg.CACertPEM) > 0 {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(cfg.CACertPEM) {
			return nil, errors.New("grafana: no valid certificates in CA bundle")
		}
		tlsCfg.RootCAs = pool
	}
	tr.TLSClientConfig = tlsCfg
	return tr, nil
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	first := via[0].URL
	if req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
		return errRedirect
	}
	return nil
}

func (c *Client) BaseURL() string {
	return c.base
}

func (c *Client) GetDashboard(ctx context.Context, uid string) (*Dashboard, error) {
	var d Dashboard
	if err := c.do(ctx, http.MethodGet, dashboardPath(uid), nil, nil, &d); err != nil {
		return nil, fmt.Errorf("get dashboard %q: %w", uid, err)
	}
	return &d, nil
}

func (c *Client) SaveDashboard(ctx context.Context, req SaveRequest) (*SaveResponse, error) {
	var resp SaveResponse
	if err := c.do(ctx, http.MethodPost, "/api/dashboards/db", nil, req, &resp); err != nil {
		return nil, fmt.Errorf("save dashboard: %w", err)
	}
	return &resp, nil
}

func (c *Client) DeleteDashboard(ctx context.Context, uid string) error {
	err := c.do(ctx, http.MethodDelete, dashboardPath(uid), nil, nil, nil)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete dashboard %q: %w", uid, err)
	}
	return nil
}

func (c *Client) SearchDashboards(ctx context.Context, folderUIDs []string) ([]SearchHit, error) {
	var all []SearchHit
	seen := make(map[string]bool)
	for page := 1; page <= maxSearchPages; page++ {
		q := url.Values{
			"type":  {"dash-db"},
			"limit": {strconv.Itoa(searchLimit)},
			"page":  {strconv.Itoa(page)},
		}
		for _, f := range folderUIDs {
			q.Add("folderUIDs", f)
		}
		var hits []SearchHit
		if err := c.do(ctx, http.MethodGet, "/api/search", q, nil, &hits); err != nil {
			return nil, fmt.Errorf("search dashboards: %w", err)
		}
		added := 0
		for _, h := range hits {
			if !seen[h.UID] {
				seen[h.UID] = true
				all = append(all, h)
				added++
			}
		}
		if len(hits) < searchLimit || added == 0 {
			return all, nil
		}
	}
	return all, nil
}

func dashboardPath(uid string) string {
	return "/api/dashboards/uid/" + url.PathEscape(uid)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	for attempt := 0; ; attempt++ {
		status, data, header, err := c.send(ctx, method, target, payload)
		if attempt < c.cfg.Retries && ctx.Err() == nil && shouldRetry(method, status, err) {
			if err := sleep(ctx, c.retryDelay(attempt, header)); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		return decode(status, data, out)
	}
}

func (c *Client) send(ctx context.Context, method, target string, payload []byte) (int, []byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, nil, err
	}
	c.setHeaders(req, payload != nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	data, err := c.readLimited(resp.Body)
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, data, resp.Header, nil
}

func (c *Client) setHeaders(req *http.Request, hasBody bool) {
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	if c.cfg.OrgID > 0 {
		req.Header.Set("X-Grafana-Org-Id", strconv.FormatInt(c.cfg.OrgID, 10))
	}
	switch auth := c.cfg.Auth; {
	case auth == "" || auth == "anonymous":
	case strings.Contains(auth, ":"):
		user, pass, _ := strings.Cut(auth, ":")
		req.SetBasicAuth(user, pass)
	default:
		req.Header.Set("Authorization", "Bearer "+auth)
	}
}

func (c *Client) readLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, c.maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > c.maxBodySize {
		return nil, fmt.Errorf("%w: exceeds %d bytes", errTooLarge, c.maxBodySize)
	}
	return data, nil
}

func decode(status int, data []byte, out any) error {
	if status == http.StatusNotFound {
		return fmt.Errorf("%w: %w", ErrNotFound, apiError(status, data))
	}
	if status < 200 || status > 299 {
		return apiError(status, data)
	}
	if out == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func apiError(status int, data []byte) *APIError {
	var body struct {
		Message string `json:"message"`
	}
	var msg string
	if json.Unmarshal(data, &body) == nil && body.Message != "" {
		msg = body.Message
	} else {
		msg = strings.TrimSpace(string(data))
	}
	if len(msg) > maxErrMessage {
		msg = strings.ToValidUTF8(msg[:maxErrMessage], "") + "…"
	}
	return &APIError{StatusCode: status, Message: msg}
}

func shouldRetry(method string, status int, err error) bool {
	if err != nil {
		return method != http.MethodPost && !errors.Is(err, errRedirect) && !errors.Is(err, errTooLarge) && !isCertError(err)
	}
	switch status {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout:
		return method != http.MethodPost
	}
	return false
}

func isCertError(err error) bool {
	var (
		verify  *tls.CertificateVerificationError
		unknown x509.UnknownAuthorityError
		host    x509.HostnameError
		invalid x509.CertificateInvalidError
	)
	return errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &host) || errors.As(err, &invalid)
}

func (c *Client) retryDelay(attempt int, h http.Header) time.Duration {
	if d, ok := c.retryAfter(h.Get("Retry-After")); ok {
		return d
	}
	d := min(c.backoffBase<<min(attempt, 20), c.backoffCap)
	return d/2 + rand.N(d/2+1) //nolint:gosec // jitter only
}

func (c *Client) retryAfter(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		switch {
		case secs < 0:
			return 0, false
		case secs > int64(c.maxRetryAfter/c.retryAfterUnit):
			return c.maxRetryAfter, true
		}
		return time.Duration(secs) * c.retryAfterUnit, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return min(max(time.Until(t), 0), c.maxRetryAfter), true
	}
	return 0, false
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
