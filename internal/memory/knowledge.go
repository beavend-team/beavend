package memory

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

var (
	// ErrTooManyZipEntries indicates a ZIP archive contains more than 1,000 entries.
	ErrTooManyZipEntries = errors.New("zip archive exceeds maximum of 1,000 entries")
	// ErrZipBombDetected indicates uncompressed extraction exceeds the 10MB cumulative limit.
	ErrZipBombDetected = errors.New("cumulative uncompressed size exceeds 10MB limit")
	// ErrTextCapExceeded indicates extracted text reached the 256KB ceiling.
	ErrTextCapExceeded = errors.New("extracted text exceeds 256KB limit")
	// ErrUnsupportedFormat indicates unhandled file extension.
	ErrUnsupportedFormat = errors.New("unsupported document format")
)

const (
	// MaxZipEntries is the maximum permitted entries in a ZIP archive (1,000).
	MaxZipEntries = 1000
	// MaxUncompressedBytes is the cumulative uncompressed size ceiling (10 MB) preventing decompression bombs.
	MaxUncompressedBytes = 10 * 1024 * 1024
	// MaxExtractedTextBytes is the maximum allowed extracted text length (256 KB).
	MaxExtractedTextBytes = 256 * 1024
	// DefaultExtractionTimeout is the maximum duration for document parsing (30 seconds).
	DefaultExtractionTimeout = 30 * time.Second
)

// ExtractTextFromBytes parses document content safely with size limits, XXE protection, and panic containment.
func ExtractTextFromBytes(ctx context.Context, filename string, data []byte) (string, error) {
	return ExtractText(ctx, filename, bytes.NewReader(data), int64(len(data)))
}

// ExtractText parses documents safely from an io.ReaderAt.
func ExtractText(ctx context.Context, filename string, r io.ReaderAt, size int64) (extracted string, err error) {
	// Panic containment guard
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("extractor panic recovered: %v", rec)
		}
	}()

	// Apply 30s timeout
	extractCtx, cancel := context.WithTimeout(ctx, DefaultExtractionTimeout)
	defer cancel()

	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".txt", ".md", ".markdown", ".json", ".csv", ".tsv":
		return extractPlainText(extractCtx, r, size)
	case ".docx":
		return extractDocx(extractCtx, r, size)
	case ".xlsx":
		return extractXlsx(extractCtx, r, size)
	case ".zip":
		return extractZip(extractCtx, r, size)
	default:
		// Attempt plain text extraction if reasonable size
		return extractPlainText(extractCtx, r, size)
	}
}

func extractPlainText(ctx context.Context, r io.ReaderAt, size int64) (string, error) {
	if size > MaxExtractedTextBytes {
		size = MaxExtractedTextBytes
	}
	buf := make([]byte, size)
	n, err := r.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read text: %w", err)
	}
	return string(buf[:n]), nil
}

// cumulativeLimiter tracks cumulative bytes read across multiple archive entries.
type cumulativeLimiter struct {
	totalRead int64
	limit     int64
}

func (l *cumulativeLimiter) wrap(r io.Reader) io.Reader {
	return &countingReader{r: r, limiter: l}
}

type countingReader struct {
	r       io.Reader
	limiter *cumulativeLimiter
}

func (cr *countingReader) Read(p []byte) (int, error) {
	if cr.limiter.totalRead >= cr.limiter.limit {
		return 0, ErrZipBombDetected
	}
	toRead := len(p)
	remaining := cr.limiter.limit - cr.limiter.totalRead
	if int64(toRead) > remaining {
		p = p[:remaining]
	}
	n, err := cr.r.Read(p)
	cr.limiter.totalRead += int64(n)
	if cr.limiter.totalRead >= cr.limiter.limit && err == nil {
		err = ErrZipBombDetected
	}
	return n, err
}

func extractDocx(ctx context.Context, r io.ReaderAt, size int64) (string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return "", fmt.Errorf("open docx zip: %w", err)
	}
	if len(zr.File) > MaxZipEntries {
		return "", ErrTooManyZipEntries
	}

	limiter := &cumulativeLimiter{limit: MaxUncompressedBytes}

	for _, f := range zr.File {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("open word/document.xml: %w", err)
			}
			defer rc.Close()

			return parseSafeXMLText(limiter.wrap(rc))
		}
	}

	return "", fmt.Errorf("word/document.xml not found in docx")
}

func extractXlsx(ctx context.Context, r io.ReaderAt, size int64) (string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return "", fmt.Errorf("open xlsx zip: %w", err)
	}
	if len(zr.File) > MaxZipEntries {
		return "", ErrTooManyZipEntries
	}

	limiter := &cumulativeLimiter{limit: MaxUncompressedBytes}

	for _, f := range zr.File {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if f.Name == "xl/sharedStrings.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", fmt.Errorf("open xl/sharedStrings.xml: %w", err)
			}
			defer rc.Close()

			return parseSafeXMLText(limiter.wrap(rc))
		}
	}

	return "", fmt.Errorf("xl/sharedStrings.xml not found in xlsx")
}

func extractZip(ctx context.Context, r io.ReaderAt, size int64) (string, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}
	if len(zr.File) > MaxZipEntries {
		return "", ErrTooManyZipEntries
	}

	limiter := &cumulativeLimiter{limit: MaxUncompressedBytes}
	var sb strings.Builder

	for _, f := range zr.File {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Skip directories and special files
		if f.FileInfo().IsDir() || strings.HasPrefix(f.Name, "__MACOSX") || strings.HasPrefix(filepath.Base(f.Name), ".") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}

		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext == ".txt" || ext == ".md" || ext == ".json" || ext == ".csv" {
			limited := limiter.wrap(rc)
			buf := make([]byte, 8192)
			for {
				n, rErr := limited.Read(buf)
				if n > 0 {
					if sb.Len()+n > MaxExtractedTextBytes {
						remaining := MaxExtractedTextBytes - sb.Len()
						sb.Write(buf[:remaining])
						_ = rc.Close()
						return sb.String(), nil
					}
					sb.Write(buf[:n])
				}
				if rErr != nil {
					break
				}
			}
			sb.WriteString("\n\n")
		}
		_ = rc.Close()
	}

	return strings.TrimSpace(sb.String()), nil
}

// parseSafeXMLText extracts text elements using a hardened XML decoder:
// Entity map is empty and Strict is true to block XML External Entity (XXE) attacks.
func parseSafeXMLText(r io.Reader) (string, error) {
	decoder := xml.NewDecoder(r)
	decoder.Strict = true
	decoder.Entity = map[string]string{} // Zero entity expansion blocks XXE attacks!

	var sb strings.Builder
	inTextTag := false

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, ErrZipBombDetected) {
				break
			}
			return "", fmt.Errorf("xml decode: %w", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" || t.Name.Local == "v" {
				inTextTag = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" || t.Name.Local == "v" {
				inTextTag = false
				sb.WriteString(" ")
			} else if t.Name.Local == "p" || t.Name.Local == "row" {
				sb.WriteString("\n")
			}
		case xml.CharData:
			if inTextTag {
				text := string(t)
				if sb.Len()+len(text) > MaxExtractedTextBytes {
					remaining := MaxExtractedTextBytes - sb.Len()
					sb.WriteString(text[:remaining])
					return strings.TrimSpace(sb.String()), nil
				}
				sb.WriteString(text)
			}
		}
	}

	return strings.TrimSpace(sb.String()), nil
}
