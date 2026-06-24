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

	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	embedder_openai "trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/openai"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source/dir"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source/file"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source/url"
	knowledgetool "trpc.group/trpc-go/trpc-agent-go/knowledge/tool"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/codefuture-io/kube-agents/pkg/config"
)

// Store manages the RAG knowledge base.
type Store struct {
	kb        *knowledge.BuiltinKnowledge
	search    tool.Tool
	uploadDir string
	s3        *S3Store
}

// NewStore creates a knowledge store from configured sources.
// Requires an OpenAI-compatible embeddings API.
func NewStore(cfg config.KnowledgeConfig, apiKey string) (*Store, error) {
	if apiKey == "" {
		slog.Warn("RAG skipped: no API key for embeddings")
		return &Store{}, nil
	}

	store := &Store{
		uploadDir: filepath.Join(os.TempDir(), "kube-agents-knowledge"),
	}

	// Initialize S3 store if configured.
	s3Store, err := NewS3Store(cfg.S3)
	if err != nil {
		slog.Warn("S3 store init failed, S3 disabled", "error", err)
	} else {
		store.s3 = s3Store
	}

	var sources []source.Source
	for _, s := range cfg.Sources {
		switch s.Type {
		case "file":
			sources = append(sources, dir.New([]string{s.Path}))
		case "url":
			sources = append(sources, url.New([]string{s.Path}))
		}
	}

	vs := inmemory.New()
	emb := embedder_openai.New(
		embedder_openai.WithAPIKey(apiKey),
		embedder_openai.WithModel(embedder_openai.DefaultModel),
	)

	kb := knowledge.New(
		knowledge.WithVectorStore(vs),
		knowledge.WithEmbedder(emb),
	)
	store.kb = kb

	ctx := context.Background()
	if len(sources) > 0 {
		for _, src := range sources {
			if err := kb.AddSource(ctx, src); err != nil {
				slog.Warn("RAG: failed to add source", "error", err)
			}
		}
		go func() {
			if err := kb.Load(ctx); err != nil {
				slog.Warn("RAG document load failed (embeddings API may not be available)", "error", err)
			} else {
				slog.Info("RAG documents loaded", "sources", len(sources))
			}
		}()
	} else {
		slog.Info("RAG store ready (no pre-configured sources, upload API available)")
	}

	search := knowledgetool.NewKnowledgeSearchTool(kb,
		knowledgetool.WithToolName("knowledge_search"),
		knowledgetool.WithToolDescription("Search K8s documentation and reference materials."),
	)
	store.search = search

	return store, nil
}

// AddFile adds a file to the knowledge base at runtime.
// The file is copied to the upload directory and indexed.
func (s *Store) AddFile(ctx interface{}, filename string, content []byte) error {
	c, _ := ctx.(context.Context)
	if c == nil {
		c = context.Background()
	}
	if s.kb == nil {
		return fmt.Errorf("knowledge base not initialized (embedding API key required)")
	}

	// Ensure upload directory exists.
	if err := os.MkdirAll(s.uploadDir, 0755); err != nil {
		return fmt.Errorf("create upload dir: %w", err)
	}

	// Write to local temp dir.
	dst := filepath.Join(s.uploadDir, filepath.Base(filename))
	if err := os.WriteFile(dst, content, 0644); err != nil {
		return fmt.Errorf("write uploaded file: %w", err)
	}

	// If S3 is configured, also upload to object storage.
	if s.s3 != nil {
		key, err := s.s3.UploadObject(c, filename, content)
		if err != nil {
			slog.Warn("S3 upload failed, file indexed locally only", "file", filename, "error", err)
		} else {
			slog.Info("RAG: file uploaded to S3", "key", key)
		}
	}

	// Add and load.
	if err := s.kb.AddSource(c, file.New([]string{dst})); err != nil {
		return fmt.Errorf("add source: %w", err)
	}
	if err := s.kb.Load(c); err != nil {
		return fmt.Errorf("load documents: %w", err)
	}

	slog.Info("RAG: file added and indexed", "file", filename, "path", dst)
	return nil
}

// SearchTool returns the knowledge search tool for agent registration.
func (s *Store) SearchTool() tool.Tool {
	return s.search
}
