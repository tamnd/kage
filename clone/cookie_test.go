package clone

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigCookieHeader(t *testing.T) {
	cases := []struct {
		name    string
		cookies []Cookie
		want    string
	}{
		{"none", nil, ""},
		{"one", []Cookie{{Name: "session", Value: "abc"}}, "session=abc"},
		{"many", []Cookie{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}}, "a=1; b=2"},
		{"skips empty name", []Cookie{{Name: "", Value: "x"}, {Name: "a", Value: "1"}}, "a=1"},
		{"empty value kept", []Cookie{{Name: "a", Value: ""}}, "a="},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Config{Cookies: tc.cookies}).CookieHeader(); got != tc.want {
				t.Errorf("CookieHeader() = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestFetchSitemapSendsCookie(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Cookie")
		_, _ = w.Write([]byte(`<urlset><url><loc>https://x/</loc></url></urlset>`))
	}))
	defer srv.Close()

	locs, _ := fetchSitemap(context.Background(), srv.Client(), "kage-test", "session=abc", srv.URL+"/sitemap.xml")
	if got != "session=abc" {
		t.Errorf("Cookie header = %q; want %q", got, "session=abc")
	}
	if len(locs) != 1 || locs[0] != "https://x/" {
		t.Errorf("locs = %v; want [https://x/]", locs)
	}
}
