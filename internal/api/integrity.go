// CLAUDE:SUMMARY Integrity API — binary SHA-256 hash, uptime, runtime stats, and health check endpoints
package api

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
)

var (
	binaryHash     string
	binaryHashOnce sync.Once
	startTime      = time.Now()
)

// computeBinaryHash calculates SHA-256 of the running binary (once).
func computeBinaryHash() string {
	binaryHashOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			binaryHash = "unknown"
			return
		}
		f, err := os.Open(exe)
		if err != nil {
			binaryHash = "unknown"
			return
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			binaryHash = "unknown"
			return
		}
		binaryHash = fmt.Sprintf("sha256:%x", h.Sum(nil))
	})
	return binaryHash
}

// BinaryHash returns the cached binary hash (call from main for startup log).
func BinaryHash() string {
	return computeBinaryHash()
}

func (a *API) RegisterIntegrityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/integrity", a.handleIntegrity)
	mux.HandleFunc("GET /api/integrity/binary", a.handleIntegrityBinary)
}

func (a *API) handleIntegrity(w http.ResponseWriter, r *http.Request) {
	nodeCount := 0
	flowStepCount := 0

	row := a.db.QueryRow("SELECT COUNT(*) FROM nodes")
	_ = row.Scan(&nodeCount)

	if a.flowsDB != nil {
		row = a.flowsDB.QueryRow("SELECT COUNT(*) FROM flow_steps")
		_ = row.Scan(&flowStepCount)
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"binary_hash":      computeBinaryHash(),
		"go_version":       runtime.Version(),
		"build_flags":      "CGO_ENABLED=0 -trimpath -ldflags=-s -w",
		"schema_version":   "v1.0",
		"uptime_seconds":   int(time.Since(startTime).Seconds()),
		"node_count":       nodeCount,
		"flow_step_count":  flowStepCount,
	})
}

func (a *API) handleIntegrityBinary(w http.ResponseWriter, r *http.Request) {
	exe, err := os.Executable()
	if err != nil {
		jsonError(w, "cannot locate binary", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="horostracker"`)
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, exe)
}
