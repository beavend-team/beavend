package secrets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecrypt(t *testing.T) {
	key := DeriveKey("my-super-secret-master-key-12345")
	secretData := []byte("sk-ant-api03-abcdef123456789")

	ciphertext, nonce, err := Encrypt(key, secretData)
	require.NoError(t, err)
	assert.NotEmpty(t, ciphertext)
	assert.NotEmpty(t, nonce)

	plaintext, err := Decrypt(key, ciphertext, nonce)
	require.NoError(t, err)
	assert.Equal(t, secretData, plaintext)

	// Tampered ciphertext
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[0] ^= 0xFF
	_, err = Decrypt(key, tampered, nonce)
	assert.Error(t, err)
}
