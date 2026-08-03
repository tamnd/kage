// Package stealth installs the same anti-detection script as
// github.com/go-rod/stealth without importing Rod's launcher-bearing root
// package.
package stealth

import (
	"github.com/go-rod/rod/lib/proto"
	"github.com/tamnd/kage/internal/rod"
)

// Page creates a page and installs the stealth script before navigation.
func Page(browser *rod.Browser) (*rod.Page, error) {
	page, err := browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, err
	}
	if _, err := page.EvalOnNewDocument(JS); err != nil {
		return nil, err
	}
	return page, nil
}
