package scaffold

import (
	"testing"

	"github.com/hurtener/dockyard/internal/manifest"
)

// TestRequiresTasksEngine is a table-driven test across the four manifest
// shapes that matter for D-164's detection: nil, empty, all-forbidden, and
// one optional/required.
func TestRequiresTasksEngine(t *testing.T) {
	tests := []struct {
		name string
		m    *manifest.Manifest
		want bool
	}{
		{
			name: "nil manifest",
			m:    nil,
			want: false,
		},
		{
			name: "empty tools",
			m:    &manifest.Manifest{},
			want: false,
		},
		{
			name: "all forbidden",
			m: &manifest.Manifest{
				Tools: []manifest.Tool{
					{Name: "a", TaskSupport: manifest.TaskSupportForbidden},
					{Name: "b", TaskSupport: manifest.TaskSupportForbidden},
				},
			},
			want: false,
		},
		{
			name: "zero-value task_support reads as forbidden",
			m: &manifest.Manifest{
				Tools: []manifest.Tool{
					{Name: "a"}, // omitted task_support
				},
			},
			want: false,
		},
		{
			name: "one optional",
			m: &manifest.Manifest{
				Tools: []manifest.Tool{
					{Name: "a", TaskSupport: manifest.TaskSupportForbidden},
					{Name: "b", TaskSupport: manifest.TaskSupportOptional},
				},
			},
			want: true,
		},
		{
			name: "one required",
			m: &manifest.Manifest{
				Tools: []manifest.Tool{
					{Name: "a", TaskSupport: manifest.TaskSupportRequired},
				},
			},
			want: true,
		},
		{
			name: "mixed required and optional",
			m: &manifest.Manifest{
				Tools: []manifest.Tool{
					{Name: "a", TaskSupport: manifest.TaskSupportRequired},
					{Name: "b", TaskSupport: manifest.TaskSupportOptional},
					{Name: "c", TaskSupport: manifest.TaskSupportForbidden},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequiresTasksEngine(tt.m); got != tt.want {
				t.Errorf("RequiresTasksEngine = %v, want %v", got, tt.want)
			}
		})
	}
}
