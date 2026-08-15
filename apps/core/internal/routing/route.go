package routing

import (
	"errors"
	"time"

	"aggregationhub.local/core/internal/credential"
	"aggregationhub.local/core/internal/provider"
)

var ErrInvalidPublicModelID = errors.New("公开模型 ID 无效")

type RoutePlan struct {
	ProviderID      string
	ProviderSlug    string
	AdapterType     string
	BaseURL         string
	UpstreamModelID string
	CredentialRef   *credential.Ref
	Capabilities    provider.Capabilities
	Timeout         time.Duration
}
