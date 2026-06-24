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

// Package config provides configuration loading for kube-agents.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration.
type Config struct {
	Server     ServerConfig    `yaml:"server"`
	Model      ModelConfig     `yaml:"model"`
	Auth       AuthConfig      `yaml:"auth"`
	Session    StoreConfig     `yaml:"session"`
	Memory     StoreConfig     `yaml:"memory"`
	Knowledge  KnowledgeConfig `yaml:"knowledge"`
	MCPServers []MCPServer     `yaml:"mcpServers"`
	Plugins    []string        `yaml:"plugins"`
	SkillsDir  string          `yaml:"skillsDir"`
}

// ServerConfig holds HTTP/gRPC/A2A server settings.
type ServerConfig struct {
	HTTP HTTPServerConfig `yaml:"http"`
	GRPC GRPCServerConfig `yaml:"grpc"`
	A2A  A2AServerConfig  `yaml:"a2a"`
}

// HTTPServerConfig is the OpenAI-compatible HTTP API config.
type HTTPServerConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// GRPCServerConfig is the gRPC API config.
type GRPCServerConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

// A2AServerConfig is the A2A server config.
type A2AServerConfig struct {
	Enabled bool   `yaml:"enabled"`
	Host    string `yaml:"host"`
}

// ModelConfig holds LLM model settings.
type ModelConfig struct {
	Provider  string `yaml:"provider"`
	Name      string `yaml:"name"`
	APIKeyEnv string `yaml:"apiKeyEnv"`
	BaseURL   string `yaml:"baseUrl"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	Mode string    `yaml:"mode"` // serviceaccount or jwt
	JWT  JWTConfig `yaml:"jwt"`
}

// JWTConfig holds JWT-specific settings.
type JWTConfig struct {
	Issuer    string `yaml:"issuer"`
	SecretEnv string `yaml:"secretEnv"`
}

// StoreConfig holds storage backend settings for session/memory.
type StoreConfig struct {
	Backend string      `yaml:"backend"` // memory or redis
	Redis   RedisConfig `yaml:"redis"`
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Addr        string `yaml:"addr"`
	PasswordEnv string `yaml:"passwordEnv"`
	DB          int    `yaml:"db"`
}

// KnowledgeConfig holds RAG knowledge store settings.
type KnowledgeConfig struct {
	Sources []KnowledgeSource `yaml:"sources"`
	S3      S3Config          `yaml:"s3"`
}

// S3Config holds S3-compatible object storage settings.
type S3Config struct {
	Enabled        bool   `yaml:"enabled"`
	Endpoint       string `yaml:"endpoint"`
	Region         string `yaml:"region"`
	AccessKey      string `yaml:"accessKey"`
	SecretKey      string `yaml:"secretKey"`
	Bucket         string `yaml:"bucket"`
	Prefix         string `yaml:"prefix"`
	AccessKeyEnv   string `yaml:"accessKeyEnv"`
	SecretKeyEnv   string `yaml:"secretKeyEnv"`
	UseSSL         bool   `yaml:"useSSL"`
	ForcePathStyle bool   `yaml:"forcePathStyle"`
}

// KnowledgeSource is a document source for the knowledge store.
type KnowledgeSource struct {
	Type string `yaml:"type"` // file or url
	Path string `yaml:"path"`
}

// MCPServer is an external MCP tool server definition.
type MCPServer struct {
	Name      string   `yaml:"name"`
	Transport string   `yaml:"transport"` // stdio, sse, streamable
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	ServerURL string   `yaml:"serverUrl"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			HTTP: HTTPServerConfig{Enabled: true, Port: 8080},
			GRPC: GRPCServerConfig{Enabled: false, Port: 9090},
			A2A:  A2AServerConfig{Enabled: false, Host: "localhost:8080"},
		},
		Model: ModelConfig{
			Provider:  "deepseek",
			Name:      "deepseek-chat",
			APIKeyEnv: "DEEPSEEK_API_KEY",
		},
		Auth: AuthConfig{
			Mode: "serviceaccount",
		},
		Session: StoreConfig{
			Backend: "memory",
			Redis:   RedisConfig{Addr: "localhost:6379", DB: 0},
		},
		Memory: StoreConfig{
			Backend: "memory",
			Redis:   RedisConfig{Addr: "localhost:6379", DB: 1},
		},
		SkillsDir: "./skills",
	}
}

// Load reads config from a YAML file, falling back to environment variables
// and defaults for unspecified values.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}
