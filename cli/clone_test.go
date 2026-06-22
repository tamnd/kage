package cli

import (
	"testing"

	"github.com/tamnd/kage/clone"
)

func TestParseCookies(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []clone.Cookie
	}{
		{"nil", nil, nil},
		{"single pair", []string{"session=abc"}, []clone.Cookie{{Name: "session", Value: "abc"}}},
		{
			"header form splits on semicolons",
			[]string{"a=1; b=2"},
			[]clone.Cookie{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}},
		},
		{
			"repeated flag accumulates",
			[]string{"a=1", "b=2"},
			[]clone.Cookie{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}},
		},
		{"value may contain equals", []string{"token=ab=cd"}, []clone.Cookie{{Name: "token", Value: "ab=cd"}}},
		{"trims whitespace", []string{"  a = 1 "}, []clone.Cookie{{Name: "a", Value: "1"}}},
		{"empty value allowed", []string{"a="}, []clone.Cookie{{Name: "a", Value: ""}}},
		{"trailing semicolon ignored", []string{"a=1;"}, []clone.Cookie{{Name: "a", Value: "1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCookies(tc.in)
			if err != nil {
				t.Fatalf("parseCookies(%q): %v", tc.in, err)
			}
			if !equalCookies(got, tc.want) {
				t.Errorf("parseCookies(%q) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseCookiesErrors(t *testing.T) {
	for _, in := range [][]string{
		{"noequalssign"},
		{"=novalue"},
		{"a=1; =bad"},
	} {
		if _, err := parseCookies(in); err == nil {
			t.Errorf("parseCookies(%q) = nil error; want error", in)
		}
	}
}

func equalCookies(a, b []clone.Cookie) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
