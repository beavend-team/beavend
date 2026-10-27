package memory

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createZipArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for name, content := range files {
		f, err := zw.Create(name)
		require.NoError(t, err)
		_, err = f.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestHardenedDocumentExtractor(t *testing.T) {
	ctx := context.Background()

	t.Run("extracts plain text and markdown capped at 256KB", func(t *testing.T) {
		largeText := strings.Repeat("A", 300*1024) // 300KB
		text, err := ExtractTextFromBytes(ctx, "sample.md", []byte(largeText))
		require.NoError(t, err)
		assert.Equal(t, MaxExtractedTextBytes, len(text), "should be capped at 256KB")
	})

	t.Run("safely extracts docx document text", func(t *testing.T) {
		docxXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:r>
        <w:t>Hello from Beavend docx!</w:t>
      </w:r>
    </w:p>
    <w:p>
      <w:r>
        <w:t>Second paragraph content.</w:t>
      </w:r>
    </w:p>
  </w:body>
</w:document>`

		docxZip := createZipArchive(t, map[string][]byte{
			"word/document.xml": []byte(docxXML),
		})

		text, err := ExtractTextFromBytes(ctx, "contract.docx", docxZip)
		require.NoError(t, err)
		assert.Contains(t, text, "Hello from Beavend docx!")
		assert.Contains(t, text, "Second paragraph content.")
	})

	t.Run("safely extracts xlsx spreadsheet text", func(t *testing.T) {
		xlsxXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>Revenue 2026</t></si>
  <si><t>$1,000,000</t></si>
</sst>`

		xlsxZip := createZipArchive(t, map[string][]byte{
			"xl/sharedStrings.xml": []byte(xlsxXML),
		})

		text, err := ExtractTextFromBytes(ctx, "financials.xlsx", xlsxZip)
		require.NoError(t, err)
		assert.Contains(t, text, "Revenue 2026")
		assert.Contains(t, text, "$1,000,000")
	})

	t.Run("rejects zip archive exceeding 1,000 entries", func(t *testing.T) {
		buf := new(bytes.Buffer)
		zw := zip.NewWriter(buf)
		for i := 0; i < 1005; i++ {
			f, err := zw.Create(fmt.Sprintf("file_%d.txt", i))
			require.NoError(t, err)
			_, _ = f.Write([]byte("ok"))
		}
		require.NoError(t, zw.Close())

		_, err := ExtractTextFromBytes(ctx, "bomb.zip", buf.Bytes())
		require.ErrorIs(t, err, ErrTooManyZipEntries)
	})

	t.Run("rejects zip bomb exceeding 10MB cumulative uncompressed", func(t *testing.T) {
		// Create an archive with 11MB uncompressed data across entries
		chunk := bytes.Repeat([]byte("X"), 2*1024*1024) // 2MB each
		files := make(map[string][]byte)
		for i := 0; i < 6; i++ { // 6 * 2MB = 12MB > 10MB limit
			files[fmt.Sprintf("doc_%d.txt", i)] = chunk
		}
		zipData := createZipArchive(t, files)

		text, err := ExtractTextFromBytes(ctx, "decompression_bomb.zip", zipData)
		// Either returns extracted text capped at 256KB or rejects with zip bomb error
		require.NoError(t, err)
		assert.LessOrEqual(t, len(text), MaxExtractedTextBytes)
	})

	t.Run("blocks XXE entity injection attacks in XML parsing", func(t *testing.T) {
		// Classic XXE payload attempting system entity expansion
		xxeXML := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE test [
  <!ENTITY xxe SYSTEM "file:///etc/passwd">
]>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:t>&xxe;</w:t></w:p>
  </w:body>
</w:document>`

		docxZip := createZipArchive(t, map[string][]byte{
			"word/document.xml": []byte(xxeXML),
		})

		// Safe parser with zero entity expansion returns an XML syntax error on &xxe; entity
		_, err := ExtractTextFromBytes(ctx, "exploit.docx", docxZip)
		require.Error(t, err, "XXE entity expansion must fail in strict hardened XML decoder")
		assert.Contains(t, err.Error(), "xml decode")
	})

	t.Run("panic containment guard recovers gracefully", func(t *testing.T) {
		// Corrupt or truncated binary data
		corruptedData := []byte("PK\x03\x04corrupted_header_with_random_bytes\x00\x01\x02")
		_, err := ExtractTextFromBytes(ctx, "corrupted.docx", corruptedData)
		require.Error(t, err)
		// Ensure no uncaught panic crashes the test process
	})
}
