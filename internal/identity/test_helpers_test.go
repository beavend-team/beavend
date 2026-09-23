package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/require"
)

type testKeyFixture struct {
	privRSA   *rsa.PrivateKey
	privJWK   jwk.Key
	pubJWK    jwk.Key
	privEC    *ecdsa.PrivateKey
	privECJWK jwk.Key
	pubECJWK  jwk.Key
	jwksSet   jwk.Set
	rsaKeyID  string
	ecKeyID   string
}

func newTestKeyFixture(t *testing.T) *testKeyFixture {
	t.Helper()

	// RSA Key
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	rsaJWK, err := jwk.FromRaw(rsaPriv)
	require.NoError(t, err)
	rsaKeyID := "test-rsa-key-1"
	require.NoError(t, rsaJWK.Set(jwk.KeyIDKey, rsaKeyID))
	require.NoError(t, rsaJWK.Set(jwk.AlgorithmKey, jwa.RS256))

	rsaPub, err := jwk.PublicKeyOf(rsaJWK)
	require.NoError(t, err)

	// ECDSA Key
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	ecJWK, err := jwk.FromRaw(ecPriv)
	require.NoError(t, err)
	ecKeyID := "test-ec-key-1"
	require.NoError(t, ecJWK.Set(jwk.KeyIDKey, ecKeyID))
	require.NoError(t, ecJWK.Set(jwk.AlgorithmKey, jwa.ES256))

	ecPub, err := jwk.PublicKeyOf(ecJWK)
	require.NoError(t, err)

	set := jwk.NewSet()
	require.NoError(t, set.AddKey(rsaPub))
	require.NoError(t, set.AddKey(ecPub))

	return &testKeyFixture{
		privRSA:   rsaPriv,
		privJWK:   rsaJWK,
		pubJWK:    rsaPub,
		privEC:    ecPriv,
		privECJWK: ecJWK,
		pubECJWK:  ecPub,
		jwksSet:   set,
		rsaKeyID:  rsaKeyID,
		ecKeyID:   ecKeyID,
	}
}

func buildAndSignToken(t *testing.T, key jwk.Key, alg jwa.SignatureAlgorithm, iss, sub string, aud []string, exp, nbf time.Time, claims map[string]any) string {
	t.Helper()

	builder := jwt.NewBuilder().
		Issuer(iss).
		Subject(sub).
		Audience(aud).
		IssuedAt(time.Now().UTC())

	if !exp.IsZero() {
		builder.Expiration(exp)
	}
	if !nbf.IsZero() {
		builder.NotBefore(nbf)
	}

	for k, v := range claims {
		builder.Claim(k, v)
	}

	token, err := builder.Build()
	require.NoError(t, err)

	signed, err := jwt.Sign(token, jwt.WithKey(alg, key))
	require.NoError(t, err)

	return string(signed)
}
