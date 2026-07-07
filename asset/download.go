package asset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Downloader fetches asset bytes over plain HTTP. It is separate from the Chrome
// pool: assets are public bytes that rarely need a real browser, so a fast HTTP
// client keeps the crawl cheap. Failures are returned to the caller, which logs
// them and moves on — a missing asset degrades a page, it never aborts a clone.
type Downloader struct {
	Client    *http.Client
	UserAgent string
	Cookie    string // value for the Cookie request header; empty sends none
	MaxBytes  int64  // per-asset cap; 0 = unlimited
	Retries   int    // extra attempts for a transient failure (0 = try once)
}

// NewDownloader builds a Downloader with a sane client and the given timeout.
func NewDownloader(userAgent string, timeout time.Duration, maxBytes int64) *Downloader {
	return &Downloader{
		Client:    &http.Client{Timeout: timeout},
		UserAgent: userAgent,
		MaxBytes:  maxBytes,
		// A few sites (and the bot-protection in front of them) reject the first
		// request of a burst with a 403 or 429 but serve a retry fine, so give
		// transient failures a couple of extra tries before giving up.
		Retries: 3,
	}
}

// Result is a downloaded asset.
type Result struct {
	Body        []byte
	ContentType string
	IsCSS       bool
}

// ErrTooLarge reports that an asset exceeds the size cap and was skipped without
// being saved. It is deliberately a skip, not a download failure: the caller
// leaves the asset out of the mirror rather than writing a truncated fragment of
// it, so a 500 MB installer or video never bloats the archive with a corrupt
// quarter of itself.
var ErrTooLarge = errors.New("asset over size cap")

// StatusError reports a non-2xx HTTP response. It carries the code so callers
// can render a clear message ("HTTP 403 Forbidden") and decide whether a retry
// is worthwhile, without the URL baked in (the caller already has it).
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	if t := http.StatusText(e.Code); t != "" {
		return fmt.Sprintf("HTTP %d %s", e.Code, t)
	}
	return fmt.Sprintf("HTTP %d", e.Code)
}

// Get fetches u, sending referer as the Referer header. It reads at most
// MaxBytes and reports whether the body is CSS (so the caller can rewrite it).
// A transient failure (a 403/429/5xx or a network blip) is retried with a short
// backoff up to Retries times.
func (d *Downloader) Get(ctx context.Context, u *url.URL, referer string) (*Result, error) {
	attempts := d.Retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if i > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(i)):
			}
		}
		res, err := d.try(ctx, u, referer)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !transient(err) {
			break
		}
	}
	return nil, lastErr
}

// try performs a single fetch attempt.
func (d *Downloader) try(ctx context.Context, u *url.URL, referer string) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if d.UserAgent != "" {
		req.Header.Set("User-Agent", d.UserAgent)
	}
	if d.Cookie != "" {
		req.Header.Set("Cookie", d.Cookie)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := d.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{Code: resp.StatusCode}
	}
	// Skip an over-cap asset instead of truncating it. A Content-Length lets us
	// bail before reading a byte; otherwise we read one byte past the cap and, if
	// the body really is larger, discard what we have. Either way nothing partial
	// reaches disk.
	if d.MaxBytes > 0 && resp.ContentLength > d.MaxBytes {
		return nil, ErrTooLarge
	}
	var r io.Reader = resp.Body
	if d.MaxBytes > 0 {
		// Read at most one byte past the cap so a body with no (or a lying)
		// Content-Length cannot stream gigabytes into memory before we notice.
		r = io.LimitReader(resp.Body, d.MaxBytes+1)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if d.MaxBytes > 0 && int64(len(body)) > d.MaxBytes {
		return nil, ErrTooLarge
	}
	ct := resp.Header.Get("Content-Type")
	return &Result{
		Body:        body,
		ContentType: ct,
		IsCSS:       isCSS(ct, u),
	}, nil
}

// backoff returns the pause before retry attempt i (1-based): 500ms, 1s, 2s, …
func backoff(i int) time.Duration {
	d := 500 * time.Millisecond << (i - 1)
	if max := 5 * time.Second; d > max {
		d = max
	}
	return d
}

// transient reports whether an error is worth retrying. Bot-protection statuses
// (403/429), request-timeout and too-early (408/425), and 5xx server errors are
// transient; other 4xx (404, 401, 410, …) are permanent. A network error is
// retried, but a cancelled or expired context is not.
func transient(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, ErrTooLarge) {
		return false
	}
	var se *StatusError
	if errors.As(err, &se) {
		switch se.Code {
		case http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
			return true
		}
		return se.Code >= 500
	}
	return true
}

// isCSS reports whether a response is a stylesheet, by content-type or by a
// .css path when the server sends no useful type.
func isCSS(contentType string, u *url.URL) bool {
	if strings.Contains(strings.ToLower(contentType), "text/css") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".css")
}
