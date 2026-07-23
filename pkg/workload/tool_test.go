package workload

import (
	"testing"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

func TestActiveImage(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name: "vdbench active",
			cfg: &config.Config{
				Tools: config.Tools{
					Active: "vdbench",
					VDBench: config.VDBench{
						Image: "quay.io/pakamble/vdbench:latest",
					},
					FIO: config.FIO{
						Image: "quay.io/ocsci/nginx:fio",
					},
				},
			},
			expected: "quay.io/pakamble/vdbench:latest",
		},
		{
			name: "fio active",
			cfg: &config.Config{
				Tools: config.Tools{
					Active: "fio",
					VDBench: config.VDBench{
						Image: "quay.io/pakamble/vdbench:latest",
					},
					FIO: config.FIO{
						Image: "quay.io/ocsci/nginx:fio",
					},
				},
			},
			expected: "quay.io/ocsci/nginx:fio",
		},
		{
			name: "no active tool defaults to fio",
			cfg: &config.Config{
				Tools: config.Tools{
					Active: "",
					VDBench: config.VDBench{
						Image: "quay.io/pakamble/vdbench:latest",
					},
					FIO: config.FIO{
						Image: "quay.io/ocsci/nginx:fio",
					},
				},
			},
			expected: "quay.io/ocsci/nginx:fio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := activeImage(tt.cfg)
			if result != tt.expected {
				t.Errorf("activeImage() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestSkipLifecycleForTool(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected bool
	}{
		{
			name: "vdbench active should skip lifecycle",
			cfg: &config.Config{
				Tools: config.Tools{
					Active: "vdbench",
				},
				Cluster: config.Cluster{
					SkipLifecycle: false,
				},
			},
			expected: true,
		},
		{
			name: "fio active with skip_lifecycle=true",
			cfg: &config.Config{
				Tools: config.Tools{
					Active: "fio",
				},
				Cluster: config.Cluster{
					SkipLifecycle: true,
				},
			},
			expected: true,
		},
		{
			name: "fio active with skip_lifecycle=false",
			cfg: &config.Config{
				Tools: config.Tools{
					Active: "fio",
				},
				Cluster: config.Cluster{
					SkipLifecycle: false,
				},
			},
			expected: false,
		},
		{
			name: "vdbench active with skip_lifecycle=true (both conditions true)",
			cfg: &config.Config{
				Tools: config.Tools{
					Active: "vdbench",
				},
				Cluster: config.Cluster{
					SkipLifecycle: true,
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := skipLifecycleForTool(tt.cfg)
			if result != tt.expected {
				t.Errorf("skipLifecycleForTool() = %v, expected %v", result, tt.expected)
			}
		})
	}
}