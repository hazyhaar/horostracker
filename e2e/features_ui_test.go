package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestFeaturesUI verifies that the new features (soft-delete, assertions,
// sources, 5W1H) are properly wired in the frontend JS and CSS.
func TestFeaturesUI(t *testing.T) {
	h, _ := ensureHarness(t)

	t.Run("AppJS_Has_DeleteButton", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			`data-action="delete"`,
			"DELETE",
			"Node supprimé",
			"btn-danger",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing delete feature element: %q", elem)
			}
		}
	})

	t.Run("AppJS_Has_DecomposeFeature", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			`data-action="decompose"`,
			"renderDecomposePanel",
			"decompose-panel",
			"validate-assertions-btn",
			"add-assertion-btn",
			"/decompose",
			"/assertions",
			"loadAssertions",
			"assertion-link",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing decompose feature element: %q", elem)
			}
		}
	})

	t.Run("AppJS_Has_SourceFeature", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			`data-action="add-source"`,
			"showSourceForm",
			"source-form",
			"source-type-select",
			"source-submit-btn",
			"loadSources",
			"source-card",
			"/source",
			"/sources",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing source feature element: %q", elem)
			}
		}
	})

	t.Run("AppJS_Has_5W1HFeature", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			"load5W1H",
			"w5h1-badges",
			"w5h1-badge",
			"w5h1-dim",
			"/5w1h",
			"Qui",
			"Quoi",
			"Quand",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing 5W1H feature element: %q", elem)
			}
		}
	})

	t.Run("CSS_Has_ClaimPieceStyles", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/css/style.css", nil, "")
		if err != nil {
			t.Fatalf("fetching style.css: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		css := string(body)

		required := []string{
			".type-claim",
			".type-piece",
			".node-type-badge.claim",
			".decompose-panel",
			".assertion-item",
			".assertion-link",
		}
		for _, elem := range required {
			if !strings.Contains(css, elem) {
				t.Errorf("style.css missing ontology style: %q", elem)
			}
		}
	})

	t.Run("CSS_Has_SourceStyles", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/css/style.css", nil, "")
		if err != nil {
			t.Fatalf("fetching style.css: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		css := string(body)

		required := []string{
			".source-form",
			".source-card",
			".source-title",
			".source-text",
		}
		for _, elem := range required {
			if !strings.Contains(css, elem) {
				t.Errorf("style.css missing source style: %q", elem)
			}
		}
	})

	t.Run("CSS_Has_5W1HStyles", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/css/style.css", nil, "")
		if err != nil {
			t.Fatalf("fetching style.css: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		css := string(body)

		required := []string{
			".w5h1-badges",
			".w5h1-badge",
			".w5h1-badge.who",
			".w5h1-badge.what",
			".w5h1-badge.when",
			".w5h1-badge.where",
			".w5h1-badge.why",
			".w5h1-badge.how",
			".w5h1-dim",
		}
		for _, elem := range required {
			if !strings.Contains(css, elem) {
				t.Errorf("style.css missing 5W1H style: %q", elem)
			}
		}
	})
}
