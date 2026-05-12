package clipboard

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"runtime"

	// Register common image decoders.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	cliperr "github.com/pastelocal/pastelocal/internal/errors"
)

func init() {
	// Register additional decoders that are not included by default.
	// BMP and TIFF are available in stdlib but not auto-registered.
	image.RegisterFormat("bmp", "BM", DecodeBMP, DecodeBMPConfig)
	image.RegisterFormat("tiff", "II*", DecodeTIFF, DecodeTIFFConfig)
}

// DecodeBMP is set to the BMP decoder from golang.org/x/image/bmp if
// available. Falls back to a stub that returns an error.
var DecodeBMP = func(r io.Reader) (image.Image, error) {
	return nil, fmt.Errorf("bmp decoder not available: add golang.org/x/image/bmp dependency")
}

// DecodeBMPConfig is set to the BMP config decoder if available.
var DecodeBMPConfig = func(r io.Reader) (image.Config, error) {
	return image.Config{}, fmt.Errorf("bmp config decoder not available")
}

// DecodeTIFF is set to the TIFF decoder from golang.org/x/image/tiff if
// available. Falls back to a stub that returns an error.
var DecodeTIFF = func(r io.Reader) (image.Image, error) {
	return nil, fmt.Errorf("tiff decoder not available: add golang.org/x/image/tiff dependency")
}

// DecodeTIFFConfig is set to the TIFF config decoder if available.
var DecodeTIFFConfig = func(r io.Reader) (image.Config, error) {
	return image.Config{}, fmt.Errorf("tiff config decoder not available")
}

// ConvertToPNG takes raw image bytes in any format that Go's image.Decode
// can handle and converts them to PNG.
//
// For HEIC images, an external tool is required:
//   - macOS: `sips` (built-in)
//   - Linux: `heif-convert` (from libheif-examples package)
//
// Returns an error with code CB1004 if the conversion fails.
func ConvertToPNG(data []byte) ([]byte, error) {
	// Try Go's built-in decoders first.
	img, _, err := image.Decode(bytes.NewReader(data))
	if err == nil {
		return encodePNG(img)
	}

	// If Go's decoders can't handle it, try HEIC via external tool.
	heicPNG, heicErr := convertHEIC(data)
	if heicErr == nil {
		return heicPNG, nil
	}

	// Neither approach worked.
	return nil, cliperr.NewWithMessage("CB1004",
		fmt.Sprintf("image conversion failed: %v", err))
}

// encodePNG encodes an image.Image as PNG bytes.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, cliperr.NewWithMessage("CB1004",
			fmt.Sprintf("png encode failed: %v", err))
	}
	return buf.Bytes(), nil
}

// convertHEIC attempts to convert HEIC data to PNG using an external tool.
// On macOS it uses `sips`; on Linux it uses `heif-convert`.
func convertHEIC(data []byte) ([]byte, error) {
	switch runtime.GOOS {
	case "darwin":
		return convertHEICSips(data)
	case "linux":
		return convertHEICHeifConvert(data)
	default:
		return nil, fmt.Errorf("heic conversion not supported on %s", runtime.GOOS)
	}
}

// convertHEICSips converts HEIC data using macOS's built-in sips tool.
func convertHEICSips(data []byte) ([]byte, error) {
	// sips needs a file path; write to a temp file.
	f, err := os.CreateTemp("", "pastelocal-*.heic")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpFile := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return nil, fmt.Errorf("writing temp file: %w", err)
	}
	f.Close()
	defer os.Remove(tmpFile)

	outFile := tmpFile + ".png"
	defer os.Remove(outFile)

	cmd := exec.Command("sips", "-s", "format", "png", tmpFile, "--out", outFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sips conversion failed: %w: %s", err, out)
	}

	result, err := os.ReadFile(outFile)
	if err != nil {
		return nil, fmt.Errorf("reading converted file: %w", err)
	}
	return result, nil
}

// convertHEICHeifConvert converts HEIC data using heif-convert on Linux.
func convertHEICHeifConvert(data []byte) ([]byte, error) {
	f, err := os.CreateTemp("", "pastelocal-*.heic")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpFile := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return nil, fmt.Errorf("writing temp file: %w", err)
	}
	f.Close()
	defer os.Remove(tmpFile)

	outFile := tmpFile + ".png"
	defer os.Remove(outFile)

	cmd := exec.Command("heif-convert", tmpFile, outFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("heif-convert failed: %w: %s", err, out)
	}

	result, err := os.ReadFile(outFile)
	if err != nil {
		return nil, fmt.Errorf("reading converted file: %w", err)
	}
	return result, nil
}
