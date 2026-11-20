package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ashleyconnor/plx/internal/logbuf"
)

const pageSize = 1000

type Client struct {
	base     string
	token    string
	clientID string
	hc       *http.Client
	log      *logbuf.Buffer
}

type Options struct {
	Insecure bool // skip TLS verification (self-signed / plex.direct certs)
	Timeout  time.Duration
	ClientID string         // X-Plex-Client-Identifier; default "plx"
	Log      *logbuf.Buffer // every request is recorded here when set
}

// New builds a client. baseURL may omit the scheme, in which case https is used.
func New(baseURL, token string, opts Options) *Client {
	if !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}
	if opts.Timeout == 0 {
		opts.Timeout = 120 * time.Second
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsConfigFor(baseURL, opts.Insecure)
	if opts.ClientID == "" {
		opts.ClientID = "plx"
	}
	return &Client{
		base:     strings.TrimRight(baseURL, "/"),
		token:    token,
		clientID: opts.ClientID,
		hc:       &http.Client{Timeout: opts.Timeout, Transport: tr},
		log:      opts.Log,
	}
}

// Params is an ordered query string. Keys are sent verbatim (Plex expects
// literal brackets in keys like label[0].tag.tag); values are escaped.
type Params []kv

type kv struct{ k, v string }

func (p Params) Add(k, v string) Params { return append(p, kv{k, v}) }

func (p Params) Encode() string {
	if len(p) == 0 {
		return ""
	}
	parts := make([]string, len(p))
	for i, e := range p {
		parts[i] = e.k + "=" + Quote(e.v)
	}
	return "?" + strings.Join(parts, "&")
}

// Quote escapes everything except unreserved characters, matching Python's
// urllib.parse.quote with an empty safe set, which plexapi uses.
func Quote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

type APIError struct {
	Method, Path string
	Status       int
	Body         string
}

// maxErrBody caps how much of an error response is kept for the log.
const maxErrBody = 64 << 10

func (e *APIError) Error() string {
	body := strings.Join(strings.Fields(e.Body), " ")
	if len(body) > 300 {
		body = body[:300] + "…"
	}
	return fmt.Sprintf("plex: %s %s: %d %s", e.Method, e.Path, e.Status, body)
}

func (c *Client) do(ctx context.Context, method, path string, p Params, out any) (err error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path+p.Encode(), nil)
	if err != nil {
		return err
	}
	if c.log != nil {
		start := time.Now()
		var status int
		var errBody string
		defer func() { c.logRequest(method, path, p, start, status, errBody, err) }()
		return c.send(req, method, path, out, &status, &errBody)
	}
	return c.send(req, method, path, out, nil, nil)
}

// send performs req, reporting the status and any error body through the
// optional pointers so do can log them.
func (c *Client) send(req *http.Request, method, path string, out any, status *int, errBody *string) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("X-Plex-Client-Identifier", c.clientID)
	req.Header.Set("X-Plex-Product", "plx")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if status != nil {
		*status = resp.StatusCode
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
		if errBody != nil {
			*errBody = string(body)
		}
		return &APIError{Method: method, Path: path, Status: resp.StatusCode, Body: string(body)}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = body
		return nil
	}
	if len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}

// retryDelays are the waits between attempts of an idempotent request.
var retryDelays = []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond, 3 * time.Second}

// doIdem is do with retries on network errors and 5xx responses. Plex
// occasionally answers 500 under load (seen on playlist moves), so every
// request that is safe to repeat goes through here. Creates and
// add-to-playlist are not: repeating them can duplicate items.
func (c *Client) doIdem(ctx context.Context, method, path string, p Params, out any) error {
	err := c.do(ctx, method, path, p, out)
	for _, d := range retryDelays {
		var apiErr *APIError
		var urlErr *url.Error
		transient := (errors.As(err, &apiErr) && apiErr.Status >= 500) || (errors.As(err, &urlErr) && !isCertError(err))
		if err == nil || ctx.Err() != nil || !transient {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
		err = c.do(ctx, method, path, p, out)
		// A DELETE that failed with 500 may still have happened.
		if method == http.MethodDelete && errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return nil
		}
	}
	return err
}

func (c *Client) get(ctx context.Context, path string, p Params) (*MediaContainer, error) {
	var env envelope
	if err := c.doIdem(ctx, http.MethodGet, path, p, &env); err != nil {
		return nil, err
	}
	return &env.MediaContainer, nil
}

// paged fetches every page of a listing. Endpoints that ignore paging (no
// totalSize in the response) return everything in the first page.
func (c *Client) paged(ctx context.Context, path string, p Params) (*MediaContainer, error) {
	var all *MediaContainer
	for start := 0; ; start += pageSize {
		pp := append(append(Params{}, p...),
			kv{"X-Plex-Container-Start", fmt.Sprint(start)},
			kv{"X-Plex-Container-Size", fmt.Sprint(pageSize)})
		mc, err := c.get(ctx, path, pp)
		if err != nil {
			return nil, err
		}
		if all == nil {
			all = mc
		} else {
			all.Metadata = append(all.Metadata, mc.Metadata...)
			all.Directory = append(all.Directory, mc.Directory...)
		}
		got := len(mc.Metadata) + len(mc.Directory)
		if mc.TotalSize == nil || got == 0 || start+got >= *mc.TotalSize {
			return all, nil
		}
	}
}

// logRequest records one request. Parameters are listed decoded, one per
// line, since Plex edit URLs are long and mostly percent-escapes. The token
// travels in a header, so nothing secret is logged.
func (c *Client) logRequest(method, path string, p Params, start time.Time, status int, errBody string, err error) {
	e := logbuf.Entry{Time: start}
	dur := time.Since(start).Round(time.Millisecond)
	switch {
	case status != 0:
		e.Summary = fmt.Sprintf("%s %s → %d %s (%s)", method, path, status, http.StatusText(status), dur)
	default:
		e.Summary = fmt.Sprintf("%s %s → failed (%s)", method, path, dur)
	}
	if err != nil {
		e.Level = logbuf.Error
	}
	for _, kv := range p {
		e.Detail = append(e.Detail, kv.k+" = "+kv.v)
	}
	if err != nil {
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			e.Detail = append(e.Detail, "error: "+err.Error())
		}
	}
	if b := strings.TrimSpace(errBody); b != "" {
		e.Detail = append(e.Detail, "response:")
		for _, l := range strings.Split(b, "\n") {
			e.Detail = append(e.Detail, "  "+l)
		}
	} else if status >= 300 {
		e.Detail = append(e.Detail, "response: (empty body)")
	}
	c.log.Add(e)
}
