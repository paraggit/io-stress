package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := NewDefault()
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse yaml %s: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse json %s: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("unsupported config extension %q (use .yaml, .yml, or .json)", ext)
	}
	return cfg, nil
}

func WriteSample(path string, force bool) error {
	cfg := NewDefault()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal sample: %w", err)
	}
	header := []byte("# odf-io-stress sample config\n# Generate: odf-io-stress generate-config\n\n")
	out := append(header, data...)
	if path == "-" {
		_, err := os.Stdout.Write(out)
		return err
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("refusing to overwrite %s (use --force)", path)
		}
	}
	return os.WriteFile(path, out, 0644)
}
