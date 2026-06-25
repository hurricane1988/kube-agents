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

package options

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codefuture-io/kube-agents/utils/log"
)

// Options holds all command-line parameters for kube-agents.
type Options struct {
	ConfigFile string
	ModelName  string
	APIKey     string
	BaseURL    string

	// Session management.
	SessionID string
	Continue  bool

	// Logging.
	Log log.Options
}

// NewOptions returns Options populated with default values.
func NewOptions() *Options {
	return &Options{
		ConfigFile: "config/kube-agents.yaml",
		ModelName:  "deepseek-chat",
		Log:        log.DefaultOptions(),
	}
}

// AddFlags binds the Options fields to cobra persistent flags.
func (o *Options) AddFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&o.ConfigFile, "config", o.ConfigFile,
		"Configuration file path")

	cmd.PersistentFlags().StringVar(&o.APIKey, "api-key", o.APIKey,
		"DeepSeek API key (env: DEEPSEEK_API_KEY, OPENAI_API_KEY)")
	cmd.PersistentFlags().StringVar(&o.BaseURL, "base-url", o.BaseURL,
		"API base URL (env: OPENAI_BASE_URL)")
	cmd.PersistentFlags().StringVar(&o.ModelName, "model", o.ModelName,
		"Model name to use")

	// Session flags.
	cmd.PersistentFlags().StringVar(&o.SessionID, "session-id", o.SessionID,
		"Session ID (resume a previous session or start new)")
	cmd.PersistentFlags().BoolVar(&o.Continue, "continue", o.Continue,
		"Continue the most recent session")

	// Log flags.
	cmd.PersistentFlags().StringVar(&o.Log.Level, "log-level", o.Log.Level,
		"Log level: debug, info, warn, error")
	cmd.PersistentFlags().StringVar(&o.Log.Format, "log-format", o.Log.Format,
		"Log format: text, json")
	cmd.PersistentFlags().BoolVar(&o.Log.AddSource, "log-add-source", o.Log.AddSource,
		"Include source file and line number in log output")
	cmd.PersistentFlags().BoolVar(&o.Log.FileOutput, "log-file-output", o.Log.FileOutput,
		"Write logs to file instead of stderr")
	cmd.PersistentFlags().StringVar(&o.Log.FilePath, "log-file-path", o.Log.FilePath,
		"Log file path (required when --log-file-output is set)")
	cmd.PersistentFlags().IntVar(&o.Log.MaxSize, "log-max-size", o.Log.MaxSize,
		"Max log file size in MB before rotation")
	cmd.PersistentFlags().IntVar(&o.Log.MaxBackups, "log-max-backups", o.Log.MaxBackups,
		"Max old log files to retain")
	cmd.PersistentFlags().IntVar(&o.Log.MaxAge, "log-max-age", o.Log.MaxAge,
		"Max days to retain old log files")
}

// Validate checks that the options are valid.
func (o *Options) Validate() error {
	if o.ModelName == "" {
		return fmt.Errorf("--model must not be empty")
	}
	return nil
}
