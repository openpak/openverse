// Package miiverse is the Store that reads and writes through the miiverse-api web bridge
// (apps/miiverse-api/src/services/web-bridge). Every call is one request to
//
//	{base}/internal/v1/web/api/v1/...
//
// with X-Internal-Key and, for a signed-in person, X-Trusted-Pid. The endpoints used are listed in
// docs/endpoints.md. When storage moves into this service, this package is replaced, not the pages.
package miiverse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/openpak/openverse/internal/store"
)

// Client implements store.Store over HTTP.
type Client struct {
	root string
	base string
	key  string
	http *http.Client

	counts  countCache
	popular popularCache
	list    listCache
}

// New builds a client. base is the miiverse-api HTTP address on the private network.
func New(base, key string) *Client {
	root := strings.TrimRight(base, "/")
	return &Client{root: root, base: root + "/internal/v1/web", key: key, http: &http.Client{Timeout: 15 * time.Second}}
}

var _ store.Store = (*Client)(nil)

// do sends one request. viewer is the pid ("" = guest). out may be nil.
func (c *Client) do(ctx context.Context, method, path, viewer string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Key", c.key)
	req.Header.Set("Content-Type", "application/json")
	if viewer != "" {
		req.Header.Set("X-Trusted-Pid", viewer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return statusError(resp.StatusCode, raw)
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func statusError(code int, raw []byte) error {
	switch code {
	case http.StatusNotFound:
		return store.ErrNotFound
	case http.StatusForbidden, http.StatusUnauthorized, http.StatusLocked:
		return store.ErrForbidden
	case http.StatusTooManyRequests:
		return store.ErrRateLimit
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s", store.ErrRejected, snippet(raw))
	}
	return fmt.Errorf("miiverse-api: status %d: %s", code, snippet(raw))
}

func snippet(b []byte) string {
	if len(b) > 200 {
		b = b[:200]
	}
	return string(b)
}

// nullable returns nil for a JSON null on a GET that the API masks as 200 null (a missing thing).
func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || string(t) == "null"
}

func q(p store.Page, extra url.Values) string {
	v := url.Values{}
	for k, vs := range extra {
		v[k] = vs
	}
	if p.Limit > 0 {
		v.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		v.Set("offset", strconv.Itoa(p.Offset))
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

// getOne GETs something that answers 200 with null (or 404) when it does not exist.
func (c *Client) getOne(ctx context.Context, path, viewer string, out any) error {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, viewer, nil, &raw); err != nil {
		return err
	}
	if isNull(raw) {
		return store.ErrNotFound
	}
	return json.Unmarshal(raw, out)
}

// Media reads one picture from the API's /cdn store (drawings, icons, headers, avatars).
func (c *Client) Media(ctx context.Context, key string) (string, []byte, error) {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.root+"/cdn/"+strings.Join(parts, "/"), nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil, store.ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("cdn: status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", nil, err
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		ct = "application/octet-stream"
	}
	return ct, b, nil
}
