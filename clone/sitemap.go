package clone

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"
	"time"
)

// sitemapDoc covers both a urlset and a sitemapindex: both carry <loc> elements,
// so one shape extracts the URLs from either.
type sitemapDoc struct {
	URLs     []sitemapEntry `xml:"url"`
	Sitemaps []sitemapEntry `xml:"sitemap"`
}

type sitemapEntry struct {
	Loc string `xml:"loc"`
}

// fetchSitemap downloads and parses one sitemap, returning page locations and,
// for a sitemap index, the child sitemap URLs.
func fetchSitemap(ctx context.Context, client *http.Client, ua, cookie, sitemapURL string) (locs, children []string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sitemapURL, nil)
	if err != nil {
		return nil, nil
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var doc sitemapDoc
	dec := xml.NewDecoder(resp.Body)
	if err := dec.Decode(&doc); err != nil {
		return nil, nil
	}
	for _, u := range doc.URLs {
		if s := strings.TrimSpace(u.Loc); s != "" {
			locs = append(locs, s)
		}
	}
	for _, sm := range doc.Sitemaps {
		if s := strings.TrimSpace(sm.Loc); s != "" {
			children = append(children, s)
		}
	}
	return locs, children
}

// collectSitemaps walks a set of seed sitemap URLs (following index files one
// level deep) and returns all discovered page locations, bounded by a deadline.
func collectSitemaps(ctx context.Context, client *http.Client, ua, cookie string, seeds []string) []string {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var out []string
	seen := map[string]bool{}
	queue := append([]string{}, seeds...)
	for len(queue) > 0 && ctx.Err() == nil {
		sm := queue[0]
		queue = queue[1:]
		if seen[sm] {
			continue
		}
		seen[sm] = true
		locs, children := fetchSitemap(ctx, client, ua, cookie, sm)
		out = append(out, locs...)
		for _, c := range children {
			if !seen[c] {
				queue = append(queue, c)
			}
		}
	}
	return out
}
