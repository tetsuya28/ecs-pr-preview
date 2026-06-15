package repository

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

type ALBInfo struct {
	ARN             string
	DNSName         string
	CanonicalZoneID string
	VpcID           string
}

type ELBV2Repository interface {
	DescribeALB(ctx context.Context, name string) (*ALBInfo, error)
	FindHTTPSListenerARN(ctx context.Context, albARN string) (string, error)
	EnsureTargetGroup(ctx context.Context, name, vpcID, healthCheckPath string) (string, error)
	EnsureListenerRule(ctx context.Context, listenerARN, domain, tgARN string, prNumber int) error
	FindListenerRuleARN(ctx context.Context, listenerARN, domain string) (string, error)
	DeleteListenerRule(ctx context.Context, ruleARN string) error
	DeleteTargetGroup(ctx context.Context, tgName string) (bool, error)
}

type elbv2Repository struct {
	client *elasticloadbalancingv2.Client
}

func NewELBV2Repository(client *elasticloadbalancingv2.Client) ELBV2Repository {
	return &elbv2Repository{client: client}
}

func (r *elbv2Repository) DescribeALB(ctx context.Context, name string) (*ALBInfo, error) {
	out, err := r.client.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{
		Names: []string{name},
	})
	if err != nil || len(out.LoadBalancers) == 0 {
		return nil, fmt.Errorf("describe ALB %s: %w", name, err)
	}
	alb := out.LoadBalancers[0]
	return &ALBInfo{
		ARN:             aws.ToString(alb.LoadBalancerArn),
		DNSName:         aws.ToString(alb.DNSName),
		CanonicalZoneID: aws.ToString(alb.CanonicalHostedZoneId),
		VpcID:           aws.ToString(alb.VpcId),
	}, nil
}

func (r *elbv2Repository) FindHTTPSListenerARN(ctx context.Context, albARN string) (string, error) {
	out, err := r.client.DescribeListeners(ctx, &elasticloadbalancingv2.DescribeListenersInput{
		LoadBalancerArn: aws.String(albARN),
	})
	if err != nil {
		return "", fmt.Errorf("describe listeners: %w", err)
	}
	for _, l := range out.Listeners {
		if aws.ToInt32(l.Port) == 443 {
			return aws.ToString(l.ListenerArn), nil
		}
	}
	return "", fmt.Errorf("HTTPS listener not found on ALB %s", albARN)
}

func (r *elbv2Repository) EnsureTargetGroup(ctx context.Context, name, vpcID, healthCheckPath string) (string, error) {
	out, err := r.client.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{
		Names: []string{name},
	})
	if err == nil && len(out.TargetGroups) > 0 {
		return aws.ToString(out.TargetGroups[0].TargetGroupArn), nil
	}

	createOut, err := r.client.CreateTargetGroup(ctx, &elasticloadbalancingv2.CreateTargetGroupInput{
		Name:                       aws.String(name),
		Protocol:                   elbv2types.ProtocolEnumHttp,
		Port:                       aws.Int32(80),
		VpcId:                      aws.String(vpcID),
		TargetType:                 elbv2types.TargetTypeEnumIp,
		HealthCheckPath:            aws.String(healthCheckPath),
		HealthCheckProtocol:        elbv2types.ProtocolEnumHttp,
		HealthCheckIntervalSeconds: aws.Int32(30),
		HealthCheckTimeoutSeconds:  aws.Int32(5),
		HealthyThresholdCount:      aws.Int32(2),
		UnhealthyThresholdCount:    aws.Int32(2),
	})
	if err != nil {
		return "", fmt.Errorf("create target group: %w", err)
	}
	tgARN := aws.ToString(createOut.TargetGroups[0].TargetGroupArn)

	if _, err := r.client.ModifyTargetGroupAttributes(ctx, &elasticloadbalancingv2.ModifyTargetGroupAttributesInput{
		TargetGroupArn: aws.String(tgARN),
		Attributes: []elbv2types.TargetGroupAttribute{
			{Key: aws.String("deregistration_delay.timeout_seconds"), Value: aws.String("30")},
		},
	}); err != nil {
		return "", fmt.Errorf("modify target group attributes: %w", err)
	}
	return tgARN, nil
}

func (r *elbv2Repository) EnsureListenerRule(ctx context.Context, listenerARN, domain, tgARN string, prNumber int) error {
	rulesOut, err := r.client.DescribeRules(ctx, &elasticloadbalancingv2.DescribeRulesInput{
		ListenerArn: aws.String(listenerARN),
	})
	if err != nil {
		return fmt.Errorf("describe rules: %w", err)
	}

	usedPriorities := make(map[int]bool)
	for _, rule := range rulesOut.Rules {
		if p := aws.ToString(rule.Priority); p != "default" {
			if n, err := strconv.Atoi(p); err == nil {
				usedPriorities[n] = true
			}
		}
		for _, c := range rule.Conditions {
			if aws.ToString(c.Field) == "host-header" {
				for _, v := range c.HostHeaderConfig.Values {
					if v == domain {
						return nil
					}
				}
			}
		}
	}

	priority := prNumber % 1000
	if priority == 0 {
		priority = 1000
	}
	for usedPriorities[priority] {
		priority++
	}
	log.Printf("  Using priority: %d", priority)

	_, err = r.client.CreateRule(ctx, &elasticloadbalancingv2.CreateRuleInput{
		ListenerArn: aws.String(listenerARN),
		Priority:    aws.Int32(int32(priority)),
		Conditions: []elbv2types.RuleCondition{{
			Field: aws.String("host-header"),
			HostHeaderConfig: &elbv2types.HostHeaderConditionConfig{
				Values: []string{domain},
			},
		}},
		Actions: []elbv2types.Action{{
			Type:           elbv2types.ActionTypeEnumForward,
			TargetGroupArn: aws.String(tgARN),
		}},
	})
	return err
}

func (r *elbv2Repository) FindListenerRuleARN(ctx context.Context, listenerARN, domain string) (string, error) {
	out, err := r.client.DescribeRules(ctx, &elasticloadbalancingv2.DescribeRulesInput{
		ListenerArn: aws.String(listenerARN),
	})
	if err != nil {
		return "", err
	}
	for _, rule := range out.Rules {
		for _, c := range rule.Conditions {
			if aws.ToString(c.Field) == "host-header" {
				for _, v := range c.HostHeaderConfig.Values {
					if v == domain {
						return aws.ToString(rule.RuleArn), nil
					}
				}
			}
		}
	}
	return "", nil
}

func (r *elbv2Repository) DeleteListenerRule(ctx context.Context, ruleARN string) error {
	_, err := r.client.DeleteRule(ctx, &elasticloadbalancingv2.DeleteRuleInput{
		RuleArn: aws.String(ruleARN),
	})
	return err
}

func (r *elbv2Repository) DeleteTargetGroup(ctx context.Context, tgName string) (bool, error) {
	out, err := r.client.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{
		Names: []string{tgName},
	})
	if err != nil || len(out.TargetGroups) == 0 {
		return false, nil
	}
	tgARN := aws.ToString(out.TargetGroups[0].TargetGroupArn)

	for i := 0; i < 12; i++ {
		health, err := r.client.DescribeTargetHealth(ctx, &elasticloadbalancingv2.DescribeTargetHealthInput{
			TargetGroupArn: aws.String(tgARN),
		})
		if err != nil || len(health.TargetHealthDescriptions) == 0 {
			break
		}
		log.Printf("  Waiting for targets to deregister (%d/12)...", i+1)
		time.Sleep(10 * time.Second)
	}

	_, err = r.client.DeleteTargetGroup(ctx, &elasticloadbalancingv2.DeleteTargetGroupInput{
		TargetGroupArn: aws.String(tgARN),
	})
	return err == nil, err
}
