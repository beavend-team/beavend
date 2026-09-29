package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1.0, 2.0, 3.0},
			b:        []float32{1.0, 2.0, 3.0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1.0, 0.0},
			b:        []float32{0.0, 1.0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1.0, 0.0},
			b:        []float32{-1.0, 0.0},
			expected: -1.0,
		},
		{
			name:     "empty vectors",
			a:        []float32{},
			b:        []float32{},
			expected: 0.0,
		},
		{
			name:     "mismatched lengths",
			a:        []float32{1.0, 2.0},
			b:        []float32{1.0},
			expected: 0.0,
		},
		{
			name:     "zero vector",
			a:        []float32{0.0, 0.0},
			b:        []float32{1.0, 2.0},
			expected: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sim := CosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, sim, 1e-4)
		})
	}
}

func TestVectorStringConversions(t *testing.T) {
	vec := []float32{0.123, -0.456, 0.789}
	str := VectorToString(vec)
	assert.Equal(t, "[0.123,-0.456,0.789]", str)

	parsed, err := StringToVector(str)
	require.NoError(t, err)
	require.Len(t, parsed, len(vec))
	for i := range vec {
		assert.InDelta(t, vec[i], parsed[i], 1e-4)
	}

	// Empty
	assert.Equal(t, "[]", VectorToString(nil))
	emptyParsed, err := StringToVector("[]")
	require.NoError(t, err)
	assert.Empty(t, emptyParsed)

	// Malformed
	_, err = StringToVector("not_a_vector")
	assert.Error(t, err)

	_, err = StringToVector("[1.0, abc]")
	assert.Error(t, err)
}

func TestPGVectorConversions(t *testing.T) {
	vec := []float32{1.5, -2.5, 3.5}
	pgv := ToPGVector(vec)
	fromPgv := FromPGVector(pgv)
	assert.Equal(t, vec, fromPgv)
}
