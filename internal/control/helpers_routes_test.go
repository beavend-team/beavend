package control

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateModelRoutesShape(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"empty ok", ``, ""},
		{"null ok", `null`, ""},
		{"valid", `{"default":"a","rules":[{"when":{"input_has":["image"]},"use":"b"}]}`, ""},
		{"missing default", `{"rules":[]}`, "default is required"},
		{"missing use", `{"default":"a","rules":[{"when":{"input_has":["image"]}}]}`, "use is required"},
		{"empty when", `{"default":"a","rules":[{"when":{},"use":"b"}]}`, "at least one condition"},
		{"bad input modality", `{"default":"a","rules":[{"when":{"input_has":["smell"]},"use":"b"}]}`, "unsupported modality"},
		{"bad output modality", `{"default":"a","rules":[{"when":{"output":["hologram"]},"use":"b"}]}`, "unsupported modality"},
		{"invalid json", `{"default":1}`, "invalid model_routes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateModelRoutesShape(json.RawMessage(tc.raw))
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestValidateModalities(t *testing.T) {
	assert.NoError(t, validateModalities("f", []string{"text", "image", "audio", "video", "document"}))
	assert.Error(t, validateModalities("f", []string{"text", "bogus"}))
	assert.NoError(t, validateModalities("f", nil))
}
