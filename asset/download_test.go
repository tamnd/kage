package asset

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestStatusErrorMessage(t *testing.T) {
	cases := map[int]string{
		403: "HTTP 403 Forbidden",
		404: "HTTP 404 Not Found",
		999: "HTTP 999",
	}
	for code, want := range cases {
		if got := (&StatusError{Code: code}).Error(); got != want {
			t.Errorf("StatusError{%d} = %q; want %q", code, got, want)
		}
	}
}

func TestGetRetriesTransientThenSucceeds(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 403 on the first try (like bot-protection), then serve the file.
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body{}"))
	}))
	defer srv.Close()

	d := NewDownloader("kage-test", 5*time.Second, 0)
	u, _ := url.Parse(srv.URL + "/style.css")
	res, err := d.Get(context.Background(), u, "")
	if err != nil {
		t.Fatalf("Get after retry: %v", err)
	}
	if !res.IsCSS || string(res.Body) != "body{}" {
		t.Errorf("unexpected result: css=%v body=%q", res.IsCSS, res.Body)
	}
	if hits < 2 {
		t.Errorf("expected a retry; server saw %d hits", hits)
	}
}

func TestGetSendsCookieHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body{}"))
	}))
	defer srv.Close()

	d := NewDownloader("kage-test", 5*time.Second, 0)
	d.Cookie = "session=abc; theme=dark"
	u, _ := url.Parse(srv.URL + "/style.css")
	if _, err := d.Get(context.Background(), u, ""); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "session=abc; theme=dark" {
		t.Errorf("Cookie header = %q; want %q", got, "session=abc; theme=dark")
	}
}

func TestGetSendsNoCookieByDefault(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Cookie")
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	d := NewDownloader("kage-test", 5*time.Second, 0)
	u, _ := url.Parse(srv.URL + "/x.png")
	if _, err := d.Get(context.Background(), u, ""); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "" {
		t.Errorf("expected no Cookie header, got %q", got)
	}
}

func TestGetDoesNotRetryPermanent(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	d := NewDownloader("kage-test", 5*time.Second, 0)
	u, _ := url.Parse(srv.URL + "/missing.png")
	_, err := d.Get(context.Background(), u, "")

	var se *StatusError
	if !errors.As(err, &se) || se.Code != 404 {
		t.Fatalf("got %v; want StatusError 404", err)
	}
	if hits != 1 {
		t.Errorf("404 should not be retried; server saw %d hits", hits)
	}
}

func TestGetGivesUpAfterRetries(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	d := NewDownloader("kage-test", 5*time.Second, 0)
	d.Retries = 2
	u, _ := url.Parse(srv.URL + "/rate.css")
	_, err := d.Get(context.Background(), u, "")

	var se *StatusError
	if !errors.As(err, &se) || se.Code != 429 {
		t.Fatalf("got %v; want StatusError 429", err)
	}
	if hits != 3 { // 1 try + 2 retries
		t.Errorf("expected 3 attempts, server saw %d", hits)
	}
}

func TestGetSkipsOverCapByContentLength(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		body := make([]byte, 4096) // declared via Content-Length
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	d := NewDownloader("kage-test", 5*time.Second, 1024) // cap below the body
	u, _ := url.Parse(srv.URL + "/clip.mp4")
	_, err := d.Get(context.Background(), u, "")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v; want ErrTooLarge", err)
	}
	if hits != 1 {
		t.Errorf("an over-cap asset should not be retried; server saw %d hits", hits)
	}
}

func TestGetSkipsOverCapWithoutContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A chunked response carries no Content-Length, so the cap is only known
		// after reading past it.
		w.Header().Set("Content-Type", "application/octet-stream")
		fl, _ := w.(http.Flusher)
		chunk := make([]byte, 512)
		for i := 0; i < 8; i++ {
			_, _ = w.Write(chunk)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	d := NewDownloader("kage-test", 5*time.Second, 1024)
	u, _ := url.Parse(srv.URL + "/stream.bin")
	_, err := d.Get(context.Background(), u, "")
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v; want ErrTooLarge", err)
	}
}

func TestGetKeepsUnderCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("small"))
	}))
	defer srv.Close()

	d := NewDownloader("kage-test", 5*time.Second, 1024)
	u, _ := url.Parse(srv.URL + "/logo.png")
	res, err := d.Get(context.Background(), u, "")
	if err != nil {
		t.Fatalf("under-cap asset: %v", err)
	}
	if string(res.Body) != "small" {
		t.Errorf("body = %q; want %q", res.Body, "small")
	}
}

func TestTransientClassification(t *testing.T) {
	transientCodes := []int{403, 408, 425, 429, 500, 502, 503}
	for _, c := range transientCodes {
		if !transient(&StatusError{Code: c}) {
			t.Errorf("status %d should be transient", c)
		}
	}
	for _, c := range []int{400, 401, 404, 410} {
		if transient(&StatusError{Code: c}) {
			t.Errorf("status %d should be permanent", c)
		}
	}
	if transient(context.Canceled) {
		t.Error("context.Canceled should not be transient")
	}
}
