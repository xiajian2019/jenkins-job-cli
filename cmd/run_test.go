package cmd

import (
	"reflect"
	"testing"

	"github.com/gocruncher/jenkins-job-cli/cmd/jj"
)

func TestParamsWithDefaults(t *testing.T) {
	params := []jj.ParameterDefinitions{
		{Name: "branch", Choices: []string{"main", "release"}},
		{Name: "message"},
	}
	params[0].DefaultParameterValue.Value = "main"
	params[1].DefaultParameterValue.Value = "use default"

	got := paramsWithDefaults(params, arguments{})
	want := map[string]string{
		"branch":  "main",
		"message": "use default",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paramsWithDefaults() = %#v, want %#v", got, want)
	}
}

func TestParamsWithDefaultsAllowsOverrides(t *testing.T) {
	params := []jj.ParameterDefinitions{{Name: "branch"}, {Name: "message"}}
	params[0].DefaultParameterValue.Value = "main"
	params[1].DefaultParameterValue.Value = "use default"

	got := paramsWithDefaults(params, arguments{args: []string{"branch=release"}})
	want := map[string]string{
		"branch":  "release",
		"message": "use default",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paramsWithDefaults() = %#v, want %#v", got, want)
	}
}

func TestSplitJobPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single job",
			input: "app-build",
			want:  []string{"app-build"},
		},
		{
			name:  "multiple jobs",
			input: "app-build,web-build",
			want:  []string{"app-build", "web-build"},
		},
		{
			name:  "trims whitespace and ignores empty entries",
			input: " app-build, , web-build,",
			want:  []string{"app-build", "web-build"},
		},
		{
			name:  "empty input",
			input: ",",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitJobPatterns(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("splitJobPatterns(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}
