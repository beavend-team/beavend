package identity

import (
	"context"
	"fmt"

	"github.com/cortexbase/cortexbase/internal/core"
)

// Provider implements core.AuthProvider using the SnapshotResolver, Verifier, and Normalizers.
type Provider struct {
	resolver   SnapshotResolver
	verifier   *Verifier
	defaultEnv string
}

// NewProvider creates a new core.AuthProvider implementation.
func NewProvider(resolver SnapshotResolver, verifier *Verifier, defaultEnv string) *Provider {
	if defaultEnv == "" {
		defaultEnv = "development"
	}
	if verifier == nil {
		verifier = NewVerifier(nil)
	}
	return &Provider{
		resolver:   resolver,
		verifier:   verifier,
		defaultEnv: defaultEnv,
	}
}

// ValidateToken validates a bearer token for a project and returns the normalized core.Identity.
func (p *Provider) ValidateToken(ctx context.Context, projectID, tokenStr string) (*core.Identity, error) {
	if p.resolver == nil {
		return nil, ErrProjectNotFound
	}

	snap, ok := p.resolver.Get(projectID, p.defaultEnv)
	if !ok {
		return nil, fmt.Errorf("%w: %s (%s)", ErrProjectNotFound, projectID, p.defaultEnv)
	}

	authCfg, hasAuth := snap.Auth()
	if !hasAuth || authCfg == nil {
		return nil, fmt.Errorf("%w: project %s", ErrAuthConfigMissing, projectID)
	}

	jwtToken, err := p.verifier.Verify(ctx, tokenStr, authCfg)
	if err != nil {
		return nil, err
	}

	normalizer := NewNormalizer(authCfg.Provider)
	return normalizer.Normalize(ctx, jwtToken, projectID, p.defaultEnv)
}

var _ core.AuthProvider = (*Provider)(nil)
