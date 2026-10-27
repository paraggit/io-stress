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
	cfg, err := decodeConfig(path, data)
	if err != nil {
		return nil, err
	}
	MergeAppSuites(cfg)
	return cfg, nil
}

func decodeConfig(path string, data []byte) (*Config, error) {
	ext := strings.ToLower(filepath.Ext(path))
	unmarshal := func(dst *Config) error {
		switch ext {
		case ".yaml", ".yml":
			if err := yaml.Unmarshal(data, dst); err != nil {
				return fmt.Errorf("parse yaml %s: %w", path, err)
			}
		case ".json":
			if err := json.Unmarshal(data, dst); err != nil {
				return fmt.Errorf("parse json %s: %w", path, err)
			}
		default:
			return fmt.Errorf("unsupported config extension %q (use .yaml, .yml, or .json)", ext)
		}
		return nil
	}
	cfg := NewDefault()
	if err := unmarshal(cfg); err != nil {
		return nil, err
	}
	// A named profile sets the object block wholesale, then the file is applied
	// again so fields written next to `profile` still win.
	profile := cfg.Tools.Object.Profile
	if profile != "" && profile != "custom" {
		base := NewDefault()
		if err := ApplyObjectProfile(&base.Tools.Object, profile); err != nil {
			return nil, err
		}
		if err := unmarshal(base); err != nil {
			return nil, err
		}
		cfg = base
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
