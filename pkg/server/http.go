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

// Package server provides HTTP, gRPC, and A2A server setup for kube-agents.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/openai"
	"trpc.group/trpc-go/trpc-agent-go/session"

	"github.com/codefuture-io/kube-agents/pkg/config"
)

const defaultShutdownTimeout = 10 * time.Second

// Server wraps an http.Server with graceful shutdown support.
type Server struct {
	*http.Server
}

// StartHTTP starts an OpenAI-compatible HTTP API server.
func StartHTTP(cfg config.HTTPServerConfig, ag agent.Agent, sessionSvc session.Service) (*Server, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	var opts []openai.Option
	opts = append(opts, openai.WithAgent(ag))
	opts = append(opts, openai.WithAppName("kube-agents"))
	if sessionSvc != nil {
		opts = append(opts, openai.WithSessionService(sessionSvc))
	}

	srv, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create HTTP server: %w", err)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	httpSrv := &http.Server{Addr: addr, Handler: srv.Handler()}

	go func() {
		slog.Info("HTTP server listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	return &Server{httpSrv}, nil
}

// StartHTTPWithRunner starts the HTTP server using a pre-built runner.
func StartHTTPWithRunner(cfg config.HTTPServerConfig, r runner.Runner, sessionSvc session.Service) (*Server, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	var opts []openai.Option
	opts = append(opts, openai.WithRunner(r))
	opts = append(opts, openai.WithAppName("kube-agents"))
	if sessionSvc != nil {
		opts = append(opts, openai.WithSessionService(sessionSvc))
	}

	srv, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create HTTP server: %w", err)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	httpSrv := &http.Server{Addr: addr, Handler: srv.Handler()}

	go func() {
		slog.Info("HTTP server listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	return &Server{httpSrv}, nil
}

// StartHTTPWithHandlers starts the HTTP server with additional custom handlers
// mounted alongside the OpenAI API handler.
func StartHTTPWithHandlers(cfg config.HTTPServerConfig, r runner.Runner, sessionSvc session.Service, extraHandlers map[string]http.Handler) (*Server, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	openaiHandler, err := buildOpenAIHandler(r, sessionSvc)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", openaiHandler)

	for path, handler := range extraHandlers {
		mux.Handle(path, handler)
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	httpSrv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		slog.Info("HTTP server listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	return &Server{httpSrv}, nil
}

func buildOpenAIHandler(r runner.Runner, sessionSvc session.Service) (http.Handler, error) {
	var opts []openai.Option
	opts = append(opts, openai.WithRunner(r))
	opts = append(opts, openai.WithAppName("kube-agents"))
	if sessionSvc != nil {
		opts = append(opts, openai.WithSessionService(sessionSvc))
	}
	srv, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create OpenAI handler: %w", err)
	}
	return srv.Handler(), nil
}

// Shutdown gracefully stops the server with a default timeout.
func (s *Server) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, defaultShutdownTimeout)
	defer cancel()

	slog.Info("HTTP server shutting down", "timeout", defaultShutdownTimeout)
	if err := s.Server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("HTTP server shutdown: %w", err)
	}
	slog.Info("HTTP server stopped")
	return nil
}
