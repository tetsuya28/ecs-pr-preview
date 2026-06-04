package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type Route53Repository interface {
	HostedZoneDomain(ctx context.Context, hostedZoneID string) (string, error)
	AliasRecordExists(ctx context.Context, hostedZoneID, domain string) (bool, error)
	UpsertAliasRecord(ctx context.Context, hostedZoneID, domain, albDNSName, albZoneID string) error
	DeleteAliasRecord(ctx context.Context, hostedZoneID, domain, albDNSName, albZoneID string) error
}

type route53Repository struct {
	client *route53.Client
}

func NewRoute53Repository(client *route53.Client) Route53Repository {
	return &route53Repository{client: client}
}

func (r *route53Repository) HostedZoneDomain(ctx context.Context, hostedZoneID string) (string, error) {
	out, err := r.client.GetHostedZone(ctx, &route53.GetHostedZoneInput{
		Id: aws.String(hostedZoneID),
	})
	if err != nil {
		return "", fmt.Errorf("get hosted zone %s: %w", hostedZoneID, err)
	}
	if out.HostedZone == nil {
		return "", fmt.Errorf("get hosted zone %s: empty hosted zone", hostedZoneID)
	}
	domain := hostedZoneDomainFromName(aws.ToString(out.HostedZone.Name))
	if domain == "" {
		return "", fmt.Errorf("get hosted zone %s: empty hosted zone name", hostedZoneID)
	}
	return domain, nil
}

func (r *route53Repository) AliasRecordExists(ctx context.Context, hostedZoneID, domain string) (bool, error) {
	out, err := r.client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(hostedZoneID),
		StartRecordName: aws.String(domain),
		StartRecordType: r53types.RRTypeA,
		MaxItems:        aws.Int32(1),
	})
	if err != nil {
		return false, fmt.Errorf("list Route53 records for %s: %w", domain, err)
	}
	if len(out.ResourceRecordSets) == 0 {
		return false, nil
	}

	record := out.ResourceRecordSets[0]
	return sameDNSName(aws.ToString(record.Name), domain) && record.Type == r53types.RRTypeA, nil
}

func (r *route53Repository) UpsertAliasRecord(ctx context.Context, hostedZoneID, domain, albDNSName, albZoneID string) error {
	return r.changeRecord(ctx, hostedZoneID, domain, albDNSName, albZoneID, r53types.ChangeActionUpsert)
}

func (r *route53Repository) DeleteAliasRecord(ctx context.Context, hostedZoneID, domain, albDNSName, albZoneID string) error {
	return r.changeRecord(ctx, hostedZoneID, domain, albDNSName, albZoneID, r53types.ChangeActionDelete)
}

func (r *route53Repository) changeRecord(ctx context.Context, hostedZoneID, domain, albDNSName, albZoneID string, action r53types.ChangeAction) error {
	_, err := r.client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(hostedZoneID),
		ChangeBatch: &r53types.ChangeBatch{
			Changes: []r53types.Change{{
				Action: action,
				ResourceRecordSet: &r53types.ResourceRecordSet{
					Name: aws.String(domain),
					Type: r53types.RRTypeA,
					AliasTarget: &r53types.AliasTarget{
						HostedZoneId:         aws.String(albZoneID),
						DNSName:              aws.String(albDNSName),
						EvaluateTargetHealth: true,
					},
				},
			}},
		},
	})
	return err
}

func hostedZoneDomainFromName(name string) string {
	return strings.TrimSuffix(strings.TrimSpace(name), ".")
}

func sameDNSName(a, b string) bool {
	return strings.EqualFold(normalizeDNSName(a), normalizeDNSName(b))
}

func normalizeDNSName(name string) string {
	return strings.TrimSuffix(strings.TrimSpace(name), ".")
}

func IsRoute53RecordNotFound(err error) bool {
	var invalidChangeBatch *r53types.InvalidChangeBatch
	if !errors.As(err, &invalidChangeBatch) {
		return false
	}

	messages := invalidChangeBatch.Messages
	if invalidChangeBatch.ErrorMessage() != "" {
		messages = append(messages, invalidChangeBatch.ErrorMessage())
	}
	if len(messages) == 0 {
		messages = append(messages, err.Error())
	}
	for _, message := range messages {
		normalized := strings.ToLower(message)
		if strings.Contains(normalized, "delete resource record set") &&
			strings.Contains(normalized, "not found") {
			return true
		}
	}
	return false
}
