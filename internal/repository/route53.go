package repository

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type Route53Repository interface {
	FindHostedZoneID(ctx context.Context, name string) (string, error)
	UpsertAliasRecord(ctx context.Context, hostedZoneID, domain, albDNSName, albZoneID string) error
	DeleteAliasRecord(ctx context.Context, hostedZoneID, domain, albDNSName, albZoneID string) error
}

type route53Repository struct {
	client *route53.Client
}

func NewRoute53Repository(client *route53.Client) Route53Repository {
	return &route53Repository{client: client}
}

func (r *route53Repository) FindHostedZoneID(ctx context.Context, name string) (string, error) {
	out, err := r.client.ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{
		DNSName: aws.String(name),
	})
	if err != nil || len(out.HostedZones) == 0 {
		return "", fmt.Errorf("hosted zone not found: %s", name)
	}
	id := aws.ToString(out.HostedZones[0].Id)
	if len(id) > 12 {
		id = id[12:] // "/hostedzone/XXXX" → "XXXX"
	}
	return id, nil
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
