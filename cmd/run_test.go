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
