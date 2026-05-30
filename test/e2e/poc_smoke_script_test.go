//go:build integration

package e2e_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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
		mu        sync.Mutex
		nextID    = 1
		uploads   = map[string][]byte{}
		deleted   = map[string]bool{}
		nameByID  = map[string]string{}
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
			if req.Name != "poc-smoke.txt" && req.Name != "multi-poc-smoke.txt" {
				t.Fatalf("expected smoke file name, got %q", req.Name)
			}
			content, err := base64.StdEncoding.DecodeString(req.ContentBase64)
			if err != nil {
				t.Fatalf("decode upload content: %v", err)
			}
			fileID := fmt.Sprintf("file-%d", nextID)
			nextID++
			uploads[fileID] = content
			nameByID[fileID] = req.Name

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_id":     fileID,
				"session_id":  "session-" + fileID,
				"chunk_count": 1,
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/") && strings.HasSuffix(r.URL.Path, "/chunks"):
			fileID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/files/"), "/chunks")
			content, ok := uploads[fileID]
			if !ok || deleted[fileID] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Chunks": []map[string]any{
					{
						"ID":     "chunk-" + fileID,
						"FileID": fileID,
						"Index":  0,
						"Offset": 0,
						"Size":   len(content),
						"Status": "available",
					},
				},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/") && strings.HasSuffix(r.URL.Path, "/download-plan"):
			fileID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/files/"), "/download-plan")
			content, ok := uploads[fileID]
			if !ok || deleted[fileID] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Plan": map[string]any{
					"FileID":     fileID,
					"Size":       len(content),
					"ChunkCount": 1,
					"Chunks": []map[string]any{
						{
							"ChunkID":          "chunk-" + fileID,
							"Index":            0,
							"Offset":           0,
							"Size":             len(content),
							"PreferredNodeID":  "node-1",
							"CandidateNodeIDs": []string{"node-1"},
						},
					},
				},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/files/"):
			fileID := strings.TrimPrefix(r.URL.Path, "/files/")
			content, ok := uploads[fileID]
			if !ok || deleted[fileID] {
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
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"File": map[string]any{
					"ID":   fileID,
					"Name": nameByID[fileID],
					"Size": len(content),
				},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/downloads/"):
			fileID := strings.TrimPrefix(r.URL.Path, "/downloads/")
			content, ok := uploads[fileID]
			if !ok || deleted[fileID] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(content)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/files/"):
			fileID := strings.TrimPrefix(r.URL.Path, "/files/")
			if _, ok := uploads[fileID]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			deleted[fileID] = true
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
		"SMOKE_LARGE_BYTES=4194432",
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
