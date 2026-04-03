package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type cartographerActionMetadata struct {
	Inputs  map[string]cartographerActionInput  `yaml:"inputs"`
	Outputs map[string]cartographerActionOutput `yaml:"outputs"`
	Runs    cartographerActionRuns              `yaml:"runs"`
}

type cartographerActionInput struct {
	Default string `yaml:"default"`
}

type cartographerActionOutput struct {
	Description string `yaml:"description"`
}

type cartographerActionRuns struct {
	Steps []cartographerActionStep `yaml:"steps"`
}

type cartographerActionStep struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Run  string `yaml:"run"`
}

func TestActionYAML_ExposesExtractionContract(t *testing.T) {
	data, err := os.ReadFile("../action.yml")
	if err != nil {
		t.Fatalf("read action.yml: %v", err)
	}

	var meta cartographerActionMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse action.yml: %v", err)
	}

	if meta.Inputs["cartographer-dir"].Default != ".cartographer" {
		t.Fatalf("cartographer-dir default = %q", meta.Inputs["cartographer-dir"].Default)
	}
	if meta.Inputs["spec-path"].Default != ".cartographer/openapi.yaml" {
		t.Fatalf("spec-path default = %q", meta.Inputs["spec-path"].Default)
	}

	for _, key := range []string{
		"cartographer-dir",
		"project-root",
		"go-version",
		"commit",
		"commit-message",
		"spec-path",
		"emit-diff",
	} {
		if _, ok := meta.Inputs[key]; !ok {
			t.Fatalf("missing action input %q", key)
		}
	}

	for _, key := range []string{
		"spec-path",
		"changed",
		"diff",
		"error",
		"operations",
	} {
		if _, ok := meta.Outputs[key]; !ok {
			t.Fatalf("missing action output %q", key)
		}
	}

	extractStep := findActionStep(meta.Runs.Steps, "extract")
	if extractStep == nil {
		t.Fatal("missing extract step")
	}
	if !strings.Contains(extractStep.Run, `--output "$SPEC_PATH"`) {
		t.Fatalf("extract step must pass spec-path to cartographer extract, got:\n%s", extractStep.Run)
	}

	statusStep := findActionStep(meta.Runs.Steps, "status")
	if statusStep == nil {
		t.Fatal("missing status step")
	}
	if !strings.Contains(statusStep.Run, `git diff --quiet -- "$SPEC_PATH"`) {
		t.Fatalf("status step must compute changes from the resolved spec path, got:\n%s", statusStep.Run)
	}

	summaryStep := findActionStep(meta.Runs.Steps, "summary")
	if summaryStep == nil {
		t.Fatal("missing summary step")
	}
	if !strings.Contains(summaryStep.Run, `"${SPEC_PATH}"`) && !strings.Contains(summaryStep.Run, `"$SPEC_PATH"`) {
		t.Fatalf("summary step must count operations from the resolved spec path, got:\n%s", summaryStep.Run)
	}
}

func findActionStep(steps []cartographerActionStep, id string) *cartographerActionStep {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i]
		}
	}
	return nil
}
