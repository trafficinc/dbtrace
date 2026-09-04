package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database DatabaseConfig      `yaml:"database"`
	Snapshot SnapshotConfig      `yaml:"snapshot"`
	Report   ReportConfig        `yaml:"report"`
	Ignore   IgnoreConfig        `yaml:"ignore"`
	Keys     map[string][]string `yaml:"keys"`
}

type DatabaseConfig struct {
	DSN string `yaml:"dsn"`
}

type SnapshotConfig struct {
	Workers   int    `yaml:"workers"`
	ChunkSize int    `yaml:"chunk_size"`
	OutputDir string `yaml:"output_dir"`
}

type ReportConfig struct {
	MaxLinesPerOperation int `yaml:"max_lines_per_operation"`
	MaxValueLength       int `yaml:"max_value_length"`
}

type IgnoreConfig struct {
	Tables  []string `yaml:"tables"`
	Columns []string `yaml:"columns"`
}

func Load(path string) (Config, error) {
	var cfg Config
	applyDefaults(&cfg)

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %q: %w", path, err)
	}
	applyDefaults(&cfg)
	if err := validate(cfg); err != nil {
		return cfg, err
	}
	cfg.Snapshot.OutputDir = filepath.Clean(cfg.Snapshot.OutputDir)
	return cfg, nil
}

func SnapshotPath(cfg Config, label string) string {
	return filepath.Join(cfg.Snapshot.OutputDir, label+".sqlite")
}

func applyDefaults(cfg *Config) {
	if cfg.Snapshot.Workers <= 0 {
		cfg.Snapshot.Workers = 4
	}
	if cfg.Snapshot.ChunkSize <= 0 {
		cfg.Snapshot.ChunkSize = 10000
	}
	if cfg.Snapshot.OutputDir == "" {
		cfg.Snapshot.OutputDir = filepath.Join(".dbtrace", "snapshots")
	}
	if cfg.Report.MaxLinesPerOperation <= 0 {
		cfg.Report.MaxLinesPerOperation = 50
	}
	if cfg.Report.MaxValueLength <= 0 {
		cfg.Report.MaxValueLength = 200
	}
}

func validate(cfg Config) error {
	if cfg.Database.DSN == "" {
		return errors.New("database.dsn is required in dbtrace.yaml")
	}
	return nil
}
