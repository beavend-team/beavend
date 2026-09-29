package memory

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/pgvector/pgvector-go"
)

// CosineSimilarity computes the cosine similarity between two float32 slices.
// Returns a value between -1.0 and 1.0 (or 0.0 if vectors are empty or orthogonal/zero).
func CosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0.0
	}

	var dot, normA, normB float64
	for i := 0; i < len(a); i++ {
		va := float64(a[i])
		vb := float64(b[i])
		dot += va * vb
		normA += va * va
		normB += vb * vb
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	sim := dot / (math.Sqrt(normA) * math.Sqrt(normB))
	if sim > 1.0 {
		sim = 1.0
	} else if sim < -1.0 {
		sim = -1.0
	}

	return float32(sim)
}

// VectorToString formats a float32 slice into a PostgreSQL vector literal "[0.1,0.2,...]".
func VectorToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, val := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(val), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// StringToVector parses a PostgreSQL vector literal "[0.1,0.2,...]" into a []float32.
func StringToVector(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("invalid vector format: %q", s)
	}
	inner := strings.Trim(s, "[]")
	if strings.TrimSpace(inner) == "" {
		return []float32{}, nil
	}

	parts := strings.Split(inner, ",")
	vec := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("invalid float at index %d (%q): %w", i, p, err)
		}
		vec[i] = float32(f)
	}
	return vec, nil
}

// ToPGVector converts []float32 into pgvector.Vector.
func ToPGVector(v []float32) pgvector.Vector {
	return pgvector.NewVector(v)
}

// FromPGVector converts pgvector.Vector into []float32.
func FromPGVector(v pgvector.Vector) []float32 {
	return v.Slice()
}
