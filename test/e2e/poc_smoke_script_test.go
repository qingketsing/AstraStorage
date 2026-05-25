//go:build integration

package e2e_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPoCSmokeScript_ExercisesCoreGatewayFlow(t *testing.T) {
	t.Parallel()

	type uploadRequest struct {
		ParentID      string `json:"parent_id"`
		Name          string `json:"name"`
		ContentType   string `json:"content_type"`
		ContentBase64 string `json:"content_base64"`
	}

	var (
		mu           sync.Mutex
		uploaded     []byte
		uploadCalled bool
		deleted      bool
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/healthz":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case r.Method == http.MethodPost && r.URL.Path == "/uploads":
			var req uploadRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode upload request: %v", err)
			}
			if req.ParentID != "root" {
				t.Fatalf("expected parent_id root, got %q", req.ParentID)
			}
			if req.Name != "poc-smoke.txt" {
				t.Fatalf("expected smoke file name, got %q", req.Name)
			}
			content, err := base64.StdEncoding.DecodeString(req.ContentBase64)
			if err != nil {
				t.Fatalf("decode upload content: %v", err)
			}
			uploaded = content
			uploadCalled = true

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_id":     "file-1",
				"session_id":  "session-1",
				"chunk_count": 1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/files/file-1":
			if deleted {
				w.WriteHeader(http.StatusBadGateway)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"code":    "mds_error",
						"message": "file missing",
					},
				})
				return
			}
			if !uploadCalled {
				t.Fatalf("metadata requested before upload")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"File": map[string]any{
					"ID":   "file-1",
					"Name": "poc-smoke.txt",
					"Size": len(uploaded),
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/downloads/file-1":
			if deleted {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(uploaded)
		case r.Method == http.MethodDelete && r.URL.Path == "/files/file-1":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	repoRoot := repoRootPath(t)
	scriptPath := filepath.Join(repoRoot, "scripts", "poc-smoke.sh")
	cmd := exec.Command("bash", scriptPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"GATEWAY_BASE_URL="+server.URL,
		"SMOKE_PARENT_ID=root",
		"SMOKE_FILE_NAME=poc-smoke.txt",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("poc smoke script failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "PoC smoke check completed.") {
		t.Fatalf("expected completion output, got:\n%s", output)
	}
}

func repoRootPath(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}
