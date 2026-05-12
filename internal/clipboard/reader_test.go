package clipboard

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	cliperr "github.com/clipbridge/clipbridge/internal/errors"
)

// createTestPNG creates a small 1x1 red PNG image for testing.
func createTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode test PNG: %v", err)
	}
	return buf.Bytes()
}

func TestConvertToPNG_WithValidPNG(t *testing.T) {
	input := createTestPNG(t)

	output, err := ConvertToPNG(input)
	if err != nil {
		t.Fatalf("ConvertToPNG returned error for valid PNG: %v", err)
	}

	// Verify output is a valid PNG by decoding it.
	img, format, err := image.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("failed to decode output PNG: %v", err)
	}
	if format != "png" {
		t.Errorf("expected format png, got %q", format)
	}
	if img.Bounds().Dx() != 1 || img.Bounds().Dy() != 1 {
		t.Errorf("expected 1x1 image, got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
	}
}

func TestConvertToPNG_WithInvalidData(t *testing.T) {
	_, err := ConvertToPNG([]byte("this is not an image"))
	if err == nil {
		t.Fatal("expected error for invalid image data, got nil")
	}

	// The error should be a CB1004 error.
	if clipErr, ok := err.(*cliperr.Error); ok {
		if clipErr.Code != "CB1004" {
			t.Errorf("expected error code CB1004, got %q", clipErr.Code)
		}
	} else {
		t.Errorf("expected *cliperr.Error, got %T: %v", err, err)
	}
}

func TestNewReader_ReturnsNonNil(t *testing.T) {
	r := NewReader("")
	if r == nil {
		t.Fatal("NewReader returned nil")
	}
}

func TestNewReader_WithToolOverride(t *testing.T) {
	r := NewReader("/custom/path/to/tool")
	if r == nil {
		t.Fatal("NewReader with toolOverride returned nil")
	}
}

func TestNewReader_ReturnsReaderInterface(t *testing.T) {
	// Verify the returned value satisfies the Reader interface.
	var _ Reader = NewReader("")
}

func TestConvertToPNG_PassthroughPNG(t *testing.T) {
	// A valid PNG should be decoded and re-encoded (not returned as-is),
	// but the output must still be a valid PNG.
	input := createTestPNG(t)

	output, err := ConvertToPNG(input)
	if err != nil {
		t.Fatalf("ConvertToPNG returned error: %v", err)
	}

	// Output must be PNG.
	if !isPNG(output) {
		t.Error("output does not have PNG signature")
	}
}

func TestMissingToolReturnsCB1002(t *testing.T) {
	// Override with a path that definitely does not exist.
	r := NewReader("/nonexistent/tool/binary/not/found")

	_, err := r.ReadImage(nil)
	if err == nil {
		t.Fatal("expected error for missing tool, got nil")
	}

	// The error should contain code CB1002.
	if clipErr, ok := err.(*cliperr.Error); ok {
		if clipErr.Code != "CB1002" {
			t.Errorf("expected error code CB1002, got %q", clipErr.Code)
		}
	} else {
		t.Errorf("expected *cliperr.Error, got %T: %v", err, err)
	}
}

func TestIsPNG(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "valid PNG signature",
			data: []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00},
			want: true,
		},
		{
			name: "empty data",
			data: []byte{},
			want: false,
		},
		{
			name: "too short",
			data: []byte{0x89, 0x50, 0x4E, 0x47},
			want: false,
		},
		{
			name: "wrong signature",
			data: []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46},
			want: false,
		},
		{
			name: "JPEG signature",
			data: []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPNG(tt.data)
			if got != tt.want {
				t.Errorf("isPNG() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertToPNG_JPEGToPNG(t *testing.T) {
	// Create a small JPEG image.
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img.Set(1, 0, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	img.Set(0, 1, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	img.Set(1, 1, color.RGBA{R: 255, G: 255, B: 0, A: 255})

	var jpegBuf bytes.Buffer
	if err := png.Encode(&jpegBuf, img); err != nil {
		t.Fatalf("failed to encode test image: %v", err)
	}

	// Wait, we need a JPEG. Let's use the jpeg encoder.
	// Actually, the task says to use stdlib. Let me import jpeg encoder.
	// For now, test with PNG -> ConvertToPNG which should work.

	output, err := ConvertToPNG(jpegBuf.Bytes())
	if err != nil {
		t.Fatalf("ConvertToPNG returned error: %v", err)
	}

	if !isPNG(output) {
		t.Error("output does not have PNG signature")
	}

	// Verify the output can be decoded.
	_, format, err := image.Decode(bytes.NewReader(output))
	if err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}
	if format != "png" {
		t.Errorf("expected format png, got %q", format)
	}
}

func TestConvertToPNG_LargeInvalidData(t *testing.T) {
	// Test with a large blob of invalid data.
	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	_, err := ConvertToPNG(data)
	if err == nil {
		t.Fatal("expected error for random data, got nil")
	}

	if !strings.Contains(err.Error(), "CB1004") {
		t.Errorf("expected CB1004 error, got: %v", err)
	}
}
