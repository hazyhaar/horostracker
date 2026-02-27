// CLAUDE:SUMMARY E2E test harness — spawns horostracker subprocess on a free port with temp data dir and HTTP helpers
package e2e

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestHarness manages a horostracker subprocess and provides HTTP helpers.
type TestHarness struct {
	BaseURL   string
	DataDir   string
	NodesDB   string
	FlowsDB   string
	MetricsDB string

	cmd    *exec.Cmd
	client *http.Client
	port   int
}

// NewHarness builds a config, starts horostracker serve, and waits for health.
func NewHarness(t *testing.T) *TestHarness {
	t.Helper()

	// Find free port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// Data directory (manual cleanup — t.TempDir() would delete files when
	// the first test finishes, breaking shared DBAssert across tests)
	dataDir, err := os.MkdirTemp("", "horostracker-e2e-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	nodesDB := filepath.Join(dataDir, "nodes.db")
	flowsDB := filepath.Join(dataDir, "flows.db")
	metricsDB := filepath.Join(dataDir, "metrics.db")

	// Write config.toml
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	config := fmt.Sprintf(`[server]
addr = ":%d"
cert_file = ""
key_file = ""

[database]
path = %q
flows_path = %q
metrics_path = %q

[auth]
jwt_secret = "e2e-test-secret-key-horostracker"
token_expiry_min = 60

[instance]
id = "e2e-test"
name = "horostracker-e2e"

[bot]
handle = "horostracker"
enabled = true
credit_per_day = 5000
default_provider = ""
default_model = ""

[federation]
enabled = false
instance_url = ""
signature_algorithm = "Ed25519"
private_key_path = ""
public_key_id = ""
verify_signatures = true
peer_instances = []

[llm]
gemini_api_key = %q
mistral_api_key = ""
openrouter_api_key = ""
groq_api_key = ""
anthropic_api_key = %q
huggingface_api_key = ""
`, port, nodesDB, flowsDB, metricsDB, geminiKey, anthropicKey)

	configPath := filepath.Join(dataDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	// Locate binary using absolute path
	wd, _ := os.Getwd()
	binary, _ := filepath.Abs(filepath.Join(wd, "..", "horostracker"))
	if _, err := os.Stat(binary); os.IsNotExist(err) {
		t.Fatalf("binary not found at %s — run: cd horostracker && CGO_ENABLED=0 go build -o horostracker .", binary)
	}

	// Start subprocess
	parentDir, _ := filepath.Abs(filepath.Join(wd, ".."))
	cmd := exec.Command(binary, "serve", "--config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = parentDir

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting horostracker: %v", err)
	}

	h := &TestHarness{
		BaseURL:   fmt.Sprintf("https://127.0.0.1:%d", port),
		DataDir:   dataDir,
		NodesDB:   nodesDB,
		FlowsDB:   flowsDB,
		MetricsDB: metricsDB,
		cmd:       cmd,
		port:      port,
		client: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}

	// Health check
	deadline := time.Now().Add(15 * time.Second)
	backoff := 100 * time.Millisecond
	for time.Now().Before(deadline) {
		resp, err := h.client.Get(h.BaseURL + "/api/bot/status")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Logf("horostracker ready on port %d", port)
				return h
			}
		}
		time.Sleep(backoff)
		if backoff < 2*time.Second {
			backoff = backoff * 3 / 2
		}
	}

	h.Stop()
	t.Fatalf("horostracker did not become ready within 15s on port %d", port)
	return nil
}

// Stop sends SIGTERM, waits 5s, then SIGKILL. Cleans up the data directory.
func (h *TestHarness) Stop() {
	if h.cmd == nil || h.cmd.Process == nil {
		return
	}
	h.cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan error, 1)
	go func() { done <- h.cmd.Wait() }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		h.cmd.Process.Kill()
		<-done
	}

	if h.DataDir != "" {
		os.RemoveAll(h.DataDir)
	}
}

// Do executes an HTTP request and returns the response.
func (h *TestHarness) Do(method, path string, body interface{}, token string) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, h.BaseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return h.client.Do(req)
}

// JSON executes a request and decodes the JSON response into dst.
func (h *TestHarness) JSON(method, path string, body interface{}, token string, dst interface{}) (*http.Response, error) {
	resp, err := h.Do(method, path, body, token)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, fmt.Errorf("reading body: %w", err)
	}

	// Reset body so caller can inspect status
	resp.Body = io.NopCloser(bytes.NewReader(data))

	if dst != nil && len(data) > 0 {
		if err := json.Unmarshal(data, dst); err != nil {
			return resp, fmt.Errorf("decoding JSON (status %d, body: %s): %w", resp.StatusCode, truncate(string(data), 500), err)
		}
	}

	return resp, nil
}

// RawBody executes a request and returns the raw response body as bytes.
func (h *TestHarness) RawBody(method, path string, body interface{}, token string) ([]byte, *http.Response, error) {
	resp, err := h.Do(method, path, body, token)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp, err
}

// Register creates a new user and returns the token and user ID.
func (h *TestHarness) Register(t *testing.T, handle, password string) (token, userID string) {
	t.Helper()
	var result struct {
		User  map[string]interface{} `json:"user"`
		Token string                 `json:"token"`
	}
	resp, err := h.JSON("POST", "/api/register", map[string]string{
		"handle":   handle,
		"password": password,
	}, "", &result)
	if err != nil {
		t.Fatalf("register %s: %v", handle, err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register %s: expected 201, got %d", handle, resp.StatusCode)
	}
	return result.Token, result.User["id"].(string)
}

// Login authenticates a user and returns the token and user ID.
func (h *TestHarness) Login(t *testing.T, handle, password string) (token, userID string) {
	t.Helper()
	var result struct {
		User  map[string]interface{} `json:"user"`
		Token string                 `json:"token"`
	}
	resp, err := h.JSON("POST", "/api/login", map[string]string{
		"handle":   handle,
		"password": password,
	}, "", &result)
	if err != nil {
		t.Fatalf("login %s: %v", handle, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s: expected 200, got %d", handle, resp.StatusCode)
	}
	return result.Token, result.User["id"].(string)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RequireStatus asserts the HTTP status code matches expected.
func RequireStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", expected, resp.StatusCode, truncate(string(body), 500))
	}
}

// bodyString reads the response body as a string (for debugging).
func bodyString(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	data, _ := io.ReadAll(resp.Body)
	return string(data)
}

// HasLLM returns true if at least one LLM API key is configured.
func HasLLM() bool {
	return os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("GEMINI_API_KEY") != ""
}

// HasAnthropic returns true if the Anthropic API key is set.
func HasAnthropic() bool {
	return os.Getenv("ANTHROPIC_API_KEY") != ""
}

// HasGemini returns true if the Gemini API key is set.
func HasGemini() bool {
	return os.Getenv("GEMINI_API_KEY") != ""
}

// AskQuestion is a helper that creates a question node and returns its ID.
func (h *TestHarness) AskQuestion(t *testing.T, token, body string, tags []string) string {
	t.Helper()
	reqBody := map[string]interface{}{"body": body}
	if len(tags) > 0 {
		reqBody["tags"] = tags
	}
	var result struct {
		Node map[string]interface{} `json:"node"`
	}
	resp, err := h.JSON("POST", "/api/ask", reqBody, token, &result)
	if err != nil {
		t.Fatalf("ask question: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ask question: expected 201, got %d", resp.StatusCode)
	}
	return result.Node["id"].(string)
}

// AnswerNode is a helper that creates a child node and returns its ID.
func (h *TestHarness) AnswerNode(t *testing.T, token, parentID, body, nodeType string) string {
	t.Helper()
	if nodeType == "" {
		nodeType = "claim"
	}
	var result map[string]interface{}
	resp, err := h.JSON("POST", "/api/answer", map[string]interface{}{
		"parent_id": parentID,
		"body":      body,
		"node_type": nodeType,
	}, token, &result)
	if err != nil {
		t.Fatalf("answer node: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("answer node: expected 201, got %d", resp.StatusCode)
	}
	return result["id"].(string)
}

// GetNode fetches a node by ID and returns the parsed JSON.
func (h *TestHarness) GetNode(t *testing.T, nodeID string) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	resp, err := h.JSON("GET", "/api/node/"+nodeID, nil, "", &result)
	if err != nil {
		t.Fatalf("get node %s: %v", nodeID, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get node %s: expected 200, got %d", nodeID, resp.StatusCode)
	}
	return result
}

// StringSlice converts an interface{} slice to a string slice.
func StringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, len(arr))
	for i, val := range arr {
		result[i] = fmt.Sprintf("%v", val)
	}
	return result
}

// ContainsString checks if a string slice contains a target.
func ContainsString(slice []string, target string) bool {
	for _, s := range slice {
		if strings.Contains(s, target) || s == target {
			return true
		}
	}
	return false
}
