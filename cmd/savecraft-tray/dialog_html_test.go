//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestRenderDialogHTMLContainsCode(t *testing.T) {
	html, err := renderDialogHTML("X9K-42M")
	if err != nil {
		t.Fatalf("renderDialogHTML: %v", err)
	}

	if !strings.Contains(html, "X9K-42M") {
		t.Error("rendered HTML should contain the link code")
	}
}

func TestRenderDialogHTMLContainsEmbeddedFonts(t *testing.T) {
	html, err := renderDialogHTML("ABC-123")
	if err != nil {
		t.Fatalf("renderDialogHTML: %v", err)
	}

	// Fonts must be base64-encoded data URIs, not external URLs.
	if !strings.Contains(html, "data:font/woff2;base64,") {
		t.Error("rendered HTML should contain base64 font data URIs")
	}

	// Must not reference Google Fonts or any external URL.
	if strings.Contains(html, "fonts.googleapis.com") {
		t.Error("rendered HTML must not reference external font URLs")
	}
}

func TestRenderDialogHTMLEscapesSpecialChars(t *testing.T) {
	// html/template should escape this, but verify.
	html, err := renderDialogHTML(`<script>alert("xss")</script>`)
	if err != nil {
		t.Fatalf("renderDialogHTML: %v", err)
	}

	if strings.Contains(html, `<script>alert("xss")</script>`) {
		t.Error("rendered HTML should escape HTML special characters in code")
	}
}
