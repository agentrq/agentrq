package image

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"strings"

	"golang.org/x/image/draw"
)

type Service interface {
	ResizeBase64(dataBase64 string, width, height int) (string, error)
}

type service struct{}

func New() Service {
	return &service{}
}

func (s *service) ResizeBase64(dataBase64 string, width, height int) (string, error) {
	if !strings.HasPrefix(dataBase64, "data:image/") {
		return dataBase64, nil // Not a base64 image or already resized
	}

	parts := strings.SplitN(dataBase64, ",", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid base64 image format")
	}

	meta := parts[0]
	raw := parts[1]

	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	img, fmtName, err := image.Decode(bytes.NewReader(decoded))
	if err != nil {
		return "", fmt.Errorf("decode image: %w", err)
	}

	// Validate squareness
	bounds := img.Bounds()
	if bounds.Dx() != bounds.Dy() {
		return "", fmt.Errorf("image must be square (current: %dx%d)", bounds.Dx(), bounds.Dy())
	}

	// Resizing
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	switch fmtName {
	case "png":
		err = png.Encode(&buf, dst)
	case "jpeg":
		err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 100})
	default:
		// Fallback to PNG if unknown (should not happen with standard image/xxx imports)
		err = png.Encode(&buf, dst)
	}

	if err != nil {
		return "", fmt.Errorf("encode image: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	// Keep the same mime type in meta part, or update it to be safe
	return meta + "," + encoded, nil
}
