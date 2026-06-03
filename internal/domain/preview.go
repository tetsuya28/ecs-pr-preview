package domain

import "fmt"

// PRPreview holds the resource names and URLs for a single PR environment.
type PRPreview struct {
	PRNumber    int
	Domain      string
	AppURL      string
	Family      string
	ServiceName string
	TGName      string
}

// NewPRPreview derives all PR resource names from the given Config.
func NewPRPreview(prNumber int, cfg Config) PRPreview {
	domain := fmt.Sprintf("%s-%d.%s", cfg.PRSubdomainPrefix, prNumber, cfg.BaseDomain)
	prefix := fmt.Sprintf("%s-%d", cfg.PRResourcePrefix, prNumber)
	return PRPreview{
		PRNumber:    prNumber,
		Domain:      domain,
		AppURL:      "https://" + domain,
		Family:      prefix,
		ServiceName: prefix,
		TGName:      prefix,
	}
}
