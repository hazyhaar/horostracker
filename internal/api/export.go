package api

import (
	"database/sql"
	"net/http"

	"github.com/hazyhaar/horostracker/internal/export"
)

// RegisterExportRoutes adds dataset export API endpoints.
func (a *API) RegisterExportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/export/tree/{id}", a.handleExportTree)
	mux.HandleFunc("GET /api/export/garbage/{id}", a.handleExportGarbageSet)
	mux.HandleFunc("GET /api/export/all", a.handleExportAll)
}

func (a *API) handleExportTree(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	// Verify tree exists
	_, err := a.db.GetNode(id)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "tree not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	exporter := export.NewExporter(a.db)
	w.Header().Set("Content-Type", "application/jsonl")
	w.Header().Set("Content-Disposition", "attachment; filename=\"tree-"+id+".jsonl\"")
	if err := exporter.ExportTree(w, id); err != nil {
		jsonError(w, "export failed: "+err.Error(), http.StatusInternalServerError)
	}
}

func (a *API) handleExportGarbageSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	// Check for existing resolution
	nodes, err := a.db.GetNodesByRoot(id)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	var resolution *string
	for _, n := range nodes {
		if n.NodeType == "resolution" {
			resolution = &n.Body
			break
		}
	}

	exporter := export.NewExporter(a.db)
	w.Header().Set("Content-Type", "application/jsonl")
	w.Header().Set("Content-Disposition", "attachment; filename=\"cgs-"+id+".jsonl\"")
	if err := exporter.ExportCorrectedGarbageSet(w, id, resolution); err != nil {
		jsonError(w, "export failed: "+err.Error(), http.StatusInternalServerError)
	}
}

func (a *API) handleExportAll(w http.ResponseWriter, r *http.Request) {
	exporter := export.NewExporter(a.db)
	w.Header().Set("Content-Type", "application/jsonl")
	w.Header().Set("Content-Disposition", "attachment; filename=\"questionsuivie-dataset.jsonl\"")
	if err := exporter.ExportAllTrees(w); err != nil {
		jsonError(w, "export failed: "+err.Error(), http.StatusInternalServerError)
	}
}
