package browser

import "testing"

func TestCookieParams(t *testing.T) {
	got := cookieParams([]Cookie{
		{Name: "session", Value: "abc", Domain: "example.com"},
		{Name: "", Value: "skipme", Domain: "example.com"}, // dropped: empty name
		{Name: "theme", Value: "dark", Domain: "example.com"},
	})
	if len(got) != 2 {
		t.Fatalf("got %d params; want 2", len(got))
	}
	if got[0].Name != "session" || got[0].Value != "abc" || got[0].Domain != "example.com" || got[0].Path != "/" {
		t.Errorf("param[0] = %+v; want session/abc/example.com//", got[0])
	}
	if got[1].Name != "theme" {
		t.Errorf("param[1].Name = %q; want theme", got[1].Name)
	}
}

func TestCookieParamsEmpty(t *testing.T) {
	if got := cookieParams(nil); got != nil && len(got) != 0 {
		t.Errorf("cookieParams(nil) = %v; want empty", got)
	}
}
