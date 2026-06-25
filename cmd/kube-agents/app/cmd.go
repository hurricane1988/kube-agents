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

package app

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/codefuture-io/kube-agents/cmd/kube-agents/app/options"
	"github.com/codefuture-io/kube-agents/internal/event"
	"github.com/codefuture-io/kube-agents/pkg/agent"
	"github.com/codefuture-io/kube-agents/pkg/config"
	"github.com/codefuture-io/kube-agents/pkg/k8s"
	k8stools "github.com/codefuture-io/kube-agents/pkg/k8s/tools"
	knowledgepkg "github.com/codefuture-io/kube-agents/pkg/knowledge"
	memorypkg "github.com/codefuture-io/kube-agents/pkg/memory"
	"github.com/codefuture-io/kube-agents/pkg/plugin"
	"github.com/codefuture-io/kube-agents/pkg/server"
	sessionpkg "github.com/codefuture-io/kube-agents/pkg/session"
	"github.com/codefuture-io/kube-agents/utils/log"
	"github.com/codefuture-io/kube-agents/version"
	"github.com/fatih/color"
)

// NewCommand returns the root cobra command for kube-agents.
func NewCommand() *cobra.Command {
	opts := options.NewOptions()

	cmd := &cobra.Command{
		Use:   "kube-agents",
		Short: "Kubernetes AI Agent powered by tRPC-Agent-Go and DeepSeek",
		Long: `kube-agents is an AI-powered Kubernetes operations assistant.
It allows users to describe K8s resource operations in natural language
and executes them via the Kubernetes API.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			log.Init(opts.Log)
			return opts.Validate()
		},
	}

	opts.AddFlags(cmd)

	cmd.AddCommand(
		newServeCommand(opts),
		newChatCommand(opts),
		newVersionCommand(),
	)

	return cmd
}

func newServeCommand(opts *options.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the kube-agents API server(s)",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load(opts.ConfigFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			return runServe(cfg, opts)
		},
	}
}

func newChatCommand(opts *options.Options) *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive chat session",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runChat(opts)
		},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Print(version.Term())
			version.Print()
		},
	}
}

func runServe(cfg config.Config, opts *options.Options) error {
	fmt.Print(version.Term())
	version.Print()

	modelName, apiKey, baseURL := resolveAuth(opts)
	if apiKey == "" {
		return fmt.Errorf("API key not provided. Set via --api-key flag or DEEPSEEK_API_KEY / OPENAI_API_KEY env var")
	}

	// Build model.
	var modelOpts []openai.Option
	modelOpts = append(modelOpts, openai.WithAPIKey(apiKey))
	if baseURL != "" {
		modelOpts = append(modelOpts, openai.WithBaseURL(baseURL))
	}
	modelOpts = append(modelOpts, openai.WithVariant(openai.VariantDeepSeek))
	modelInstance := openai.New(modelName, modelOpts...)

	// Build tools: calculator + K8s if available.
	tools := []tool.Tool{agent.CalculatorTool()}

	k8sClients, k8sErr := k8s.NewClients()
	if k8sErr != nil {
		slog.Warn("K8s client unavailable, K8s tools disabled", "error", k8sErr)
	} else {
		tools = append(tools, k8stools.MustNewToolSet(k8sClients)...)
		slog.Info("K8s tools registered", "namespace", k8sClients.Namespace)
	}

	// Knowledge store (built-in text search, no embedding API needed).
	knowledgeStore, _ := knowledgepkg.NewStore(cfg.Knowledge, apiKey)
	if knowledgeStore != nil && knowledgeStore.SearchTool() != nil {
		tools = append(tools, knowledgeStore.SearchTool())
		slog.Info("knowledge_search tool registered")
	}

	genConfig := model.GenerationConfig{
		Stream:      true,
		Temperature: ptrOf(0.7),
	}

	llmAgent := agent.MustNewLLMAgent(modelInstance, tools, genConfig)

	// Session and memory services.
	sessionSvc, err := sessionpkg.NewService(cfg.Session)
	if err != nil {
		return fmt.Errorf("session service: %w", err)
	}
	memorySvc, err := memorypkg.NewService(cfg.Memory)
	if err != nil {
		return fmt.Errorf("memory service: %w", err)
	}

	// Plugins.
	pluginReg := plugin.CreatePlugins(cfg.Plugins)

	// Runner.
	runnerOpts := []runner.Option{runner.WithSessionService(sessionSvc)}
	if memorySvc != nil {
		runnerOpts = append(runnerOpts, runner.WithMemoryService(memorySvc))
	}
	if plugins := pluginReg.Build(); len(plugins) > 0 {
		runnerOpts = append(runnerOpts, runner.WithPlugins(plugins...))
		slog.Info("Plugins loaded", "names", pluginReg.Names())
	}
	r := runner.NewRunner("kube-agents-app", llmAgent, runnerOpts...)
	defer r.Close()

	var httpSrv *server.Server
	if cfg.Server.HTTP.Enabled {
		extraHandlers := map[string]http.Handler{}
		if knowledgeStore != nil {
			extraHandlers["/v1/knowledge/upload"] = server.KnowledgeUploadHandler(knowledgeStore)
			extraHandlers["/v1/knowledge/files"] = server.KnowledgeListHandler(knowledgeStore)
			extraHandlers["/v1/sessions"] = server.SessionListHandler(sessionSvc)
			extraHandlers["/v1/sessions/"] = server.SessionDeleteHandler(sessionSvc)
		}
		httpSrv, err = server.StartHTTPWithHandlers(cfg.Server.HTTP, r, sessionSvc, extraHandlers)
		if err != nil {
			return fmt.Errorf("HTTP server: %w", err)
		}
	}

	if cfg.Server.A2A.Enabled {
		if err := server.StartA2AWithRunner(cfg.Server.A2A, r, sessionSvc); err != nil {
			return fmt.Errorf("A2A server: %w", err)
		}
	}

	// Wait for shutdown signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	slog.Info("Received shutdown signal")
	if httpSrv != nil {
		if err := httpSrv.Shutdown(context.Background()); err != nil {
			slog.Error("HTTP shutdown error", "error", err)
		}
	}
	r.Close()
	slog.Info("Shutdown complete")
	return nil
}

func runChat(opts *options.Options) error {
	modelName, apiKey, baseURL := resolveAuth(opts)

	if apiKey == "" {
		return fmt.Errorf("API key not provided. Set via --api-key flag or DEEPSEEK_API_KEY / OPENAI_API_KEY env var")
	}

	// Session management.
	sessionID := opts.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	// Build model.
	var modelOpts []openai.Option
	modelOpts = append(modelOpts, openai.WithAPIKey(apiKey))
	if baseURL != "" {
		modelOpts = append(modelOpts, openai.WithBaseURL(baseURL))
	}
	modelOpts = append(modelOpts, openai.WithVariant(openai.VariantDeepSeek))
	modelInstance := openai.New(modelName, modelOpts...)

	// Build tools: calculator + K8s if available.
	tools := []tool.Tool{agent.CalculatorTool()}
	var k8sNamespace string
	k8sClients, k8sErr := k8s.NewClients()
	if k8sErr != nil {
		slog.Warn("K8s client unavailable, K8s tools disabled", "error", k8sErr)
	} else {
		tools = append(tools, k8stools.MustNewToolSet(k8sClients)...)
		k8sNamespace = k8sClients.Namespace
		slog.Debug("K8s tools registered", "namespace", k8sNamespace)
	}

	// Knowledge store (built-in text search, no embedding API needed).
	knowledgeStore, _ := knowledgepkg.NewStore(config.DefaultConfig().Knowledge, apiKey)
	if knowledgeStore != nil && knowledgeStore.SearchTool() != nil {
		tools = append(tools, knowledgeStore.SearchTool())
		slog.Debug("knowledge_search tool registered")
	}

	// Print banner after initialization so we can show namespace, etc.
	printChatBanner(modelName, baseURL, sessionID, k8sNamespace)

	genConfig := model.GenerationConfig{
		Stream:      true,
		Temperature: ptrOf(0.7),
	}

	llmAgent := agent.MustNewLLMAgent(modelInstance, tools, genConfig)
	r := runner.NewRunner("kube-agents-app", llmAgent)
	defer r.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return interactiveLoop(ctx, r, sessionID)
}

// printChatBanner prints a Claude Code-style two-column startup banner for chat mode.
func printChatBanner(modelName, baseURL, sessionID, k8sNamespace string) {
	const LW = 38 // left column width (visible columns)
	const RW = 58 // right column width (visible columns)
	const CW = LW + 3 + RW

	// Color function — only applied to art chars, never to padding spaces.
	icon := color.New(color.FgHiCyan, color.Bold).SprintFunc()

	// Build an exactly LW-wide centered art string, then colorize only the art chars.
	art := func(raw string) string {
		n := len([]rune(raw))
		left := (LW - n) / 2
		right := LW - left - n
		return strings.Repeat(" ", left) + icon(raw) + strings.Repeat(" ", right)
	}

	// rowPlain: both columns are plain text, padded by fmt.
	rowPlain := func(left, right string) {
		fmt.Printf("│ %-*s │ %-*s │\n", LW, left, RW, right)
	}

	// rowArt: left is pre-padded (may contain ANSI codes), right is padded by fmt.
	rowArt := func(left, right string) {
		fmt.Printf("│ %s │ %-*s │\n", left, RW, right)
	}

	// Top border.
	title := fmt.Sprintf(" kube-agents %s ", version.Version)
	total := CW + 2
	fmt.Printf("╭───%s%s╮\n", title, strings.Repeat("─", total-4-len(title)))

	centerText := func(s string) string {
		n := len(s)
		return strings.Repeat(" ", (LW-n)/2) + s
	}

	rowPlain("", "")
	rowPlain(centerText("Welcome Back!"), "Tips for getting started")
	rowPlain("", "/help    Show available commands")
	rowArt(art("▐⎈██⎈▜▌"), "/exit    Exit and save session")
	rowArt(art("▝▜█████▛▘"), strings.Repeat("─", RW))
	rowArt(art("▘▘ ▝▝"), "Session")
	rowPlain("", "Resume:  chat --session-id <id>")
	rowPlain("", "Clear:   chat --clear-session <id>")
	rowPlain("", "")
	rowPlain("   Model: "+modelName, "")
	if k8sNamespace != "" {
		rowPlain("   Namespace: "+k8sNamespace, "")
	}
	rowPlain("   Session: "+truncateSessionID(sessionID), "")
	cwd, _ := os.Getwd()
	rowPlain("   "+shortenPath(cwd), "")

	// Bottom border.
	fmt.Printf("╰%s╯\n", strings.Repeat("─", total))
}

// truncateSessionID returns a shortened session ID like "f9025794...cc6bbffadfe6".
func truncateSessionID(id string) string {
	if len(id) <= 20 {
		return id
	}
	return id[:8] + "..." + id[len(id)-12:]
}

// shortenPath replaces the home dir prefix with ~ for display.
func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		path = strings.Replace(path, home, "~", 1)
	}
	return path
}

func interactiveLoop(ctx context.Context, r runner.Runner, sessionID string) error {
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "/exit" {
			fmt.Printf("Goodbye! To resume this session:\n")
			fmt.Printf("  kube-agents chat --api-key=... --session-id=%s\n", sessionID)
			break
		}

		events, err := r.Run(ctx, "user-001", sessionID, model.NewUserMessage(input))
		if err != nil {
			slog.Error("Run error", "error", err)
			continue
		}

		fmt.Print("\n")
		proc := event.NewStreamProcessor()
		for ev := range events {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			if text := proc.Process(ev); text != "" {
				fmt.Print(text)
			}
		}
		if final := proc.Finalize(); final != "" {
			fmt.Print(final)
		}
		fmt.Println()
	}
	if err := scanner.Err(); err != nil {
		slog.Error("Input read error", "error", err)
	}
	return nil
}

func resolveAuth(opts *options.Options) (modelName, apiKey, baseURL string) {
	modelName = opts.ModelName
	apiKey = opts.APIKey
	baseURL = opts.BaseURL

	if apiKey == "" {
		apiKey = os.Getenv("DEEPSEEK_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if baseURL == "" {
		baseURL = os.Getenv("OPENAI_BASE_URL")
	}
	return
}

func ptrOf[T any](v T) *T {
	return &v
}
