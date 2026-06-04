package repository

import "testing"

func TestTaskDefinitionFamilyFromARN(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want string
	}{
		{
			name: "task definition ARN",
			arn:  "arn:aws:ecs:ap-northeast-1:123456789012:task-definition/myapp-pr-1:42",
			want: "myapp-pr-1",
		},
		{
			name: "family revision",
			arn:  "myapp-pr-1:42",
			want: "myapp-pr-1",
		},
		{
			name: "family only",
			arn:  "myapp-pr-1",
			want: "myapp-pr-1",
		},
		{
			name: "does not trim prefix-like family",
			arn:  "arn:aws:ecs:ap-northeast-1:123456789012:task-definition/myapp-pr-10:1",
			want: "myapp-pr-10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskDefinitionFamilyFromARN(tt.arn); got != tt.want {
				t.Fatalf("taskDefinitionFamilyFromARN(%q) = %q, want %q", tt.arn, got, tt.want)
			}
		})
	}
}
