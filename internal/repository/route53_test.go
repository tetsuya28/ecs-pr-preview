package repository

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

func TestHostedZoneDomainFromName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "route53 dns name", in: "example.com.", want: "example.com"},
		{name: "already normalized", in: "example.com", want: "example.com"},
		{name: "trims whitespace", in: " example.com. ", want: "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hostedZoneDomainFromName(tt.in); got != tt.want {
				t.Fatalf("hostedZoneDomainFromName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsRoute53RecordNotFound(t *testing.T) {
	err := &r53types.InvalidChangeBatch{
		Message: aws.String("ChangeBatch errors occurred"),
		Messages: []string{
			"Tried to delete resource record set [name='pr-1319.example.com.', type='A'] but it was not found",
		},
	}

	if !IsRoute53RecordNotFound(err) {
		t.Fatal("expected record not found")
	}
}

func TestIsRoute53RecordNotFoundRejectsOtherErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "different route53 invalid change",
			err: &r53types.InvalidChangeBatch{
				Message:  aws.String("ChangeBatch errors occurred"),
				Messages: []string{"Tried to create resource record set but it already exists"},
			},
		},
		{
			name: "generic error",
			err:  errors.New("not found"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsRoute53RecordNotFound(tt.err) {
				t.Fatal("expected false")
			}
		})
	}
}
