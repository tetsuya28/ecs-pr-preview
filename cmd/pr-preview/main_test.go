package main

import "testing"

func TestValidatePRNumber(t *testing.T) {
	tests := []struct {
		name     string
		prNumber int
		wantErr  bool
	}{
		{name: "zero", prNumber: 0},
		{name: "positive", prNumber: 123},
		{name: "negative", prNumber: -1, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePRNumber(tt.prNumber)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validatePRNumber: %v", err)
			}
		})
	}
}

func TestBuildGitHubCommenter(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		repository string
		prNumber   int
		want       bool
	}{
		{name: "valid", token: "ghs_test", repository: "owner/repo", prNumber: 123, want: true},
		{name: "missing token", repository: "owner/repo", prNumber: 123},
		{name: "missing repository", token: "ghs_test", prNumber: 123},
		{name: "invalid repository", token: "ghs_test", repository: "owner", prNumber: 123},
		{name: "negative pr number", token: "ghs_test", repository: "owner/repo", prNumber: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildGitHubCommenter(tt.token, tt.repository, tt.prNumber)
			if tt.want && got == nil {
				t.Fatal("expected commenter")
			}
			if !tt.want && got != nil {
				t.Fatal("expected nil commenter")
			}
		})
	}
}
