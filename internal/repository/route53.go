package repository

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	r53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type Route53Repository interface {
	UpsertAliasRecord(ctx context.Context, hostedZoneID, domain, albDNSName, albZoneID string) error
	DeleteAliasRecord(ctx context.Context, hostedZoneID, domain, albDNSName, albZoneID string) error
}

type route53Repository struct {
	client *route53.Client
}

func NewRoute53Repository(client *route53.Client) Route53Repository {
	return &route53Repository{client: client}
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
