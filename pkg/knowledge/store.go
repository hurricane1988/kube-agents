/*
Copyright 2026 CodeFuture Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package knowledge

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/codefuture-io/kube-agents/pkg/config"
)

// Store manages the knowledge base with built-in file-based search.
// Does NOT require an embedding API — works with any LLM backend.
type Store struct {
	search    tool.Tool
	uploadDir string
	s3        *S3Store
}

// NewStore creates a knowledge store from configured sources.
// Uses simple text matching search — no embedding API required.
func NewStore(cfg config.KnowledgeConfig, apiKey string) (*Store, error) {
	uploadDir := filepath.Join(os.TempDir(), "kube-agents-knowledge")
	if d := os.Getenv("KNOWLEDGE_UPLOAD_DIR"); d != "" {
		uploadDir = d
	}
	store := &Store{
		uploadDir: uploadDir,
	}

	// Initialize S3 store if configured.
	s3Store, err := NewS3Store(cfg.S3)
	if err != nil {
		slog.Warn("S3 store init failed, S3 disabled", "error", err)
	} else {
		store.s3 = s3Store
	}

	// Collect all document paths from configured sources.
	var docPaths []string
	for _, s := range cfg.Sources {
		switch s.Type {
		case "file":
			paths, _ := collectFiles(s.Path)
			docPaths = append(docPaths, paths...)
		}
	}

	if len(docPaths) > 0 {
		slog.Debug("knowledge store initialized with local files", "count", len(docPaths))
	} else {
		slog.Debug("knowledge store ready (no pre-configured sources, upload API available)")
	}

	// Create the built-in search tool using simple text matching.
	store.search = function.NewFunctionTool(
		func(ctx context.Context, req SearchReq) (SearchRsp, error) {
			return searchDocs(docPaths, req)
		},
		function.WithName("knowledge_search"),
		function.WithDescription(
			"Search Kubernetes documentation and reference materials. "+
				"Use this to find information about K8s concepts, commands, and best practices. "+
				"Provide one or more search keywords.",
		),
	)

	return store, nil
}

// SearchReq is the input for the knowledge search tool.
type SearchReq struct {
	Query string `json:"query" jsonschema:"description=search keywords or question,required"`
}

// SearchRsp is the output.
type SearchRsp struct {
	Results []SearchResult `json:"results"`
	Count   int            `json:"count"`
}

// SearchResult is a single match.
type SearchResult struct {
	Source  string `json:"source"`
	Snippet string `json:"snippet"`
}

// AddFile adds a file to the knowledge base at runtime.
// If S3 is configured, the file is uploaded to {prefix}/{sessionID}/{filename} in the bucket.
// Otherwise it is stored in the local upload directory.
func (s *Store) AddFile(ctx interface{}, sessionID, filename string, content []byte) error {
	c, _ := ctx.(context.Context)
	if c == nil {
		c = context.Background()
	}

	if sessionID == "" {
		sessionID = "default"
	}

	var storedPath string

	if s.s3 != nil {
		// S3-first: upload to object storage under session directory.
		key, err := s.s3.UploadObject(c, sessionID, filename, content)
		if err != nil {
			return fmt.Errorf("S3 upload: %w", err)
		}
		storedPath = "s3://" + s.s3.Bucket() + "/" + key
	} else {
		// Fallback: write to local temp dir organized by session.
		sessionDir := filepath.Join(s.uploadDir, sessionID)
		if err := os.MkdirAll(sessionDir, 0755); err != nil {
			return fmt.Errorf("create session dir: %w", err)
		}
		dst := filepath.Join(sessionDir, filepath.Base(filename))
		if err := os.WriteFile(dst, content, 0644); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		storedPath = dst
	}

	slog.Info("knowledge file added",
		"file", filename,
		"session", sessionID,
		"size", len(content),
		"path", storedPath,
	)
	return nil
}

// SearchTool returns the knowledge search tool for agent registration.
func (s *Store) SearchTool() tool.Tool {
	return s.search
}

// UploadDir returns the directory where uploaded files are stored.
func (s *Store) UploadDir() string {
	return s.uploadDir
}

// ListFiles returns files currently in the upload directory.
func (s *Store) ListFiles() []string {
	entries, err := os.ReadDir(s.uploadDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// --- built-in text search ---

func searchDocs(paths []string, req SearchReq) (SearchRsp, error) {
	if req.Query == "" {
		return SearchRsp{}, nil
	}

	terms := strings.Fields(strings.ToLower(req.Query))
	var results []SearchResult

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		lower := strings.ToLower(content)

		// Score: count matching terms.
		score := 0
		for _, term := range terms {
			score += strings.Count(lower, term)
		}
		if score == 0 {
			continue
		}

		// Extract relevant snippet around first match.
		snippet := extractSnippet(content, lower, terms[0], 300)
		results = append(results, SearchResult{
			Source:  filepath.Base(path),
			Snippet: fmt.Sprintf("(score=%d) %s", score, snippet),
		})
	}

	// Sort by score descending (simple bubble — fine for small doc sets).
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if len(results[i].Snippet) < len(results[j].Snippet) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > 5 {
		results = results[:5]
	}
	return SearchRsp{Results: results, Count: len(results)}, nil
}

func extractSnippet(content, lower, firstTerm string, window int) string {
	idx := strings.Index(lower, firstTerm)
	if idx < 0 {
		if len(content) > window {
			return content[:window] + "..."
		}
		return content
	}
	start := idx - window/2
	if start < 0 {
		start = 0
	}
	end := start + window
	if end > len(content) {
		end = len(content)
	}
	snippet := content[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}
	return snippet
}

func collectFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".md", ".txt", ".yaml", ".yml":
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}
