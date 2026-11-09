package models

import "testing"

func TestGoldenCriteriaValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		golden []GoldenCriterion
		valid  bool
	}{
		{"valid", []GoldenCriterion{{Criterion: "tests pass", CheckCommand: "go test ./..."}}, true},
		{"unknown", []GoldenCriterion{{Criterion: "other", CheckCommand: "true"}}, false},
		{"missing command", []GoldenCriterion{{Criterion: "tests pass"}}, false},
		{"duplicate", []GoldenCriterion{{Criterion: "tests pass", CheckCommand: "true"}, {Criterion: "tests pass", CheckCommand: "true"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateGoldenCriteria([]string{"tests pass"}, tc.golden)
			if (err == nil) != tc.valid {
				t.Fatalf("validation error = %v; valid = %t", err, tc.valid)
			}
		})
	}
}
