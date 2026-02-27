package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestSources(t *testing.T) {
	h, dba := ensureHarness(t)
	ownerToken, _ := h.Register(t, "src_owner", "srcowner12345")

	// Create a question and assertions to attach sources to
	questionID := h.AskQuestion(t, ownerToken, "Les perturbateurs endocriniens dans l'eau potable sont-ils un risque avéré pour la santé publique ?", []string{"santé", "environnement"})

	var assertResult struct {
		Nodes []map[string]interface{} `json:"nodes"`
	}
	h.JSON("POST", "/api/node/"+questionID+"/assertions", map[string]interface{}{
		"assertions": []string{
			"Les perturbateurs endocriniens sont présents dans l'eau potable en France",
			"Les concentrations détectées dépassent les seuils de l'OMS",
		},
	}, ownerToken, &assertResult)

	assertionID := assertResult.Nodes[0]["id"].(string)
	assertionID2 := assertResult.Nodes[1]["id"].(string)
	_ = assertionID2

	t.Run("AddSourceWithText", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var source map[string]interface{}
		resp, err := h.JSON("POST", "/api/node/"+assertionID+"/source", map[string]interface{}{
			"content_text": "Étude ANSES 2023 : détection de bisphénol A dans 87% des échantillons d'eau potable testés en Île-de-France. Concentrations moyennes de 0.12 µg/L, soit 2.4x le seuil recommandé par l'OMS.",
			"title":        "Rapport ANSES 2023 - Perturbateurs endocriniens",
		}, ownerToken, &source)
		if err != nil {
			t.Fatalf("add source: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if source["id"] == nil || source["id"] == "" {
			t.Error("source should have an id")
		}
		if source["content_text"] == nil {
			t.Error("source should have content_text")
		}
		if source["content_hash"] == nil {
			t.Error("source should have content_hash")
		}
	})

	t.Run("AddSourceWithURL", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var source map[string]interface{}
		resp, err := h.JSON("POST", "/api/node/"+assertionID+"/source", map[string]interface{}{
			"url":   "https://www.anses.fr/fr/content/perturbateurs-endocriniens-2023",
			"title": "ANSES - Perturbateurs endocriniens",
		}, ownerToken, &source)
		if err != nil {
			t.Fatalf("add source url: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if source["url"] == nil {
			t.Error("source should have url")
		}
		if source["domain"] != "www.anses.fr" {
			t.Errorf("domain = %v, want www.anses.fr", source["domain"])
		}
	})

	t.Run("GetSourcesForNode", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Sources []map[string]interface{} `json:"sources"`
			Count   int                      `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/node/"+assertionID+"/sources", nil, "", &result)
		if err != nil {
			t.Fatalf("get sources: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if result.Count < 2 {
			t.Errorf("count = %d, want >= 2", result.Count)
		}
	})

	t.Run("SourceOnQuestion", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var source map[string]interface{}
		resp, err := h.JSON("POST", "/api/node/"+questionID+"/source", map[string]interface{}{
			"content_text": "Source directement versée sur la question",
			"title":        "Source question",
		}, ownerToken, &source)
		if err != nil {
			t.Fatalf("add source on question: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)
	})

	t.Run("SourceOnPiece", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		pieceID := h.AnswerNode(t, ownerToken, questionID, "Piece with source", "piece")

		var source map[string]interface{}
		resp, err := h.JSON("POST", "/api/node/"+pieceID+"/source", map[string]interface{}{
			"content_text": "Source attachée à la pièce",
		}, ownerToken, &source)
		if err != nil {
			t.Fatalf("add source on piece: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)
	})

	t.Run("SourceAcceptedOnClaim", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		claimID := h.AnswerNode(t, ownerToken, questionID, "Claim that accepts sources", "claim")
		var source map[string]interface{}
		resp, err := h.JSON("POST", "/api/node/"+claimID+"/source", map[string]interface{}{
			"content_text": "Source on claim node",
		}, ownerToken, &source)
		if err != nil {
			t.Fatalf("add source on claim: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)
	})

	t.Run("SourceRequiresContent", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/node/"+assertionID+"/source", map[string]interface{}{
			"title": "Source without content or URL",
		}, ownerToken)
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("SourceRequiresAuth", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/node/"+assertionID+"/source", map[string]interface{}{
			"content_text": "Unauthenticated source",
		}, "")
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("EmptySourcesForNewNode", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		newQ := h.AskQuestion(t, ownerToken, "Question sans sources", nil)
		var result struct {
			Sources []interface{} `json:"sources"`
			Count   int           `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/node/"+newQ+"/sources", nil, "", &result)
		if err != nil {
			t.Fatalf("get sources: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if result.Count != 0 {
			t.Errorf("count = %d, want 0", result.Count)
		}
	})

	// --- DB assertions ---

	t.Run("DB_Sources_Table_Has_ContentText", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		db, _ := dba.nodes()
		rows, err := db.Query("PRAGMA table_info(sources)")
		if err != nil {
			t.Fatalf("pragma: %v", err)
		}
		defer rows.Close()

		found := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull int
			var dflt interface{}
			var pk int
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				t.Fatalf("scanning: %v", err)
			}
			if name == "content_text" {
				found = true
			}
		}
		if !found {
			t.Error("sources table missing content_text column")
		}
	})

	t.Run("DB_Sources_ContentHash_Set", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		db, _ := dba.nodes()
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM sources WHERE content_hash IS NOT NULL AND node_id = ?", assertionID).Scan(&count)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if count == 0 {
			t.Error("no sources with content_hash found")
		}
	})
}
