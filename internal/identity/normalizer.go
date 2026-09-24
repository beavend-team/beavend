package identity

import (
	"context"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwt"

	"github.com/beavend/beavend/internal/core"
)

// Normalizer transforms a validated JWT into the normalized core.Identity.
type Normalizer interface {
	Normalize(ctx context.Context, token jwt.Token, project, env string) (*core.Identity, error)
}

// NewNormalizer returns the appropriate Normalizer implementation for the provider.
func NewNormalizer(provider string) Normalizer {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "firebase":
		return &FirebaseNormalizer{}
	case "supabase":
		return &SupabaseNormalizer{}
	default:
		return &GenericNormalizer{}
	}
}

// GenericNormalizer normalizes standard OIDC tokens into core.Identity.
type GenericNormalizer struct{}

func (n *GenericNormalizer) Normalize(ctx context.Context, token jwt.Token, project, env string) (*core.Identity, error) {
	rawClaims, err := token.AsMap(ctx)
	if err != nil {
		return nil, err
	}

	claims := cloneAndFlattenClaims(rawClaims)

	aud := ""
	if auds := token.Audience(); len(auds) > 0 {
		aud = strings.Join(auds, ",")
	}

	id := &core.Identity{
		Project:     project,
		Environment: env,
		Issuer:      token.Issuer(),
		Subject:     token.Subject(),
		Audience:    aud,
		Claims:      claims,
	}

	return id, nil
}

// FirebaseNormalizer normalizes Firebase Auth tokens into core.Identity.
type FirebaseNormalizer struct {
	generic GenericNormalizer
}

func (n *FirebaseNormalizer) Normalize(ctx context.Context, token jwt.Token, project, env string) (*core.Identity, error) {
	id, err := n.generic.Normalize(ctx, token, project, env)
	if err != nil {
		return nil, err
	}

	// Firebase-specific claim guarantees
	if _, ok := id.Claims["user_id"]; !ok && id.Subject != "" {
		id.Claims["user_id"] = id.Subject
	}

	// Extract top-level sign_in_provider shortcut if present under firebase.sign_in_provider
	if providerVal, ok := id.Claims["firebase.sign_in_provider"]; ok {
		if _, exists := id.Claims["sign_in_provider"]; !exists {
			id.Claims["sign_in_provider"] = providerVal
		}
	} else if fbRaw, ok := id.Claims["firebase"]; ok {
		if fbMap, isMap := fbRaw.(map[string]any); isMap {
			if p, hasP := fbMap["sign_in_provider"]; hasP {
				id.Claims["sign_in_provider"] = p
			}
		}
	}

	return id, nil
}

// SupabaseNormalizer normalizes Supabase Auth tokens into core.Identity.
type SupabaseNormalizer struct {
	generic GenericNormalizer
}

func (n *SupabaseNormalizer) Normalize(ctx context.Context, token jwt.Token, project, env string) (*core.Identity, error) {
	id, err := n.generic.Normalize(ctx, token, project, env)
	if err != nil {
		return nil, err
	}

	// In Supabase, if role is present, ensure it is available top-level
	if roleVal, ok := id.Claims["role"]; ok {
		id.Claims["role"] = roleVal
	}

	return id, nil
}

// cloneAndFlattenClaims deep copies the raw claims map and flattens nested maps into dot-notation paths.
func cloneAndFlattenClaims(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src)*2)
	for k, v := range src {
		cloned := cloneValue(v)
		dst[k] = cloned
		if m, ok := cloned.(map[string]any); ok {
			flattenMap(k, m, dst)
		}
	}
	return dst
}

func flattenMap(prefix string, src map[string]any, dst map[string]any) {
	for k, v := range src {
		fullPath := prefix + "." + k
		dst[fullPath] = cloneValue(v)
		if subMap, ok := v.(map[string]any); ok {
			flattenMap(fullPath, subMap, dst)
		}
	}
}

// cloneValue performs a deep copy of primitive types, slices, and maps.
func cloneValue(v any) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case map[string]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			res[k] = cloneValue(item)
		}
		return res
	case []any:
		res := make([]any, len(val))
		for i, item := range val {
			res[i] = cloneValue(item)
		}
		return res
	case []string:
		res := make([]string, len(val))
		copy(res, val)
		return res
	default:
		// Go primitives (strings, numbers, bools) are value types and immutable
		return val
	}
}
