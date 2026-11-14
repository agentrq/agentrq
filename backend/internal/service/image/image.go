// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type Service interface {
	ResizeBase64(dataBase64 string, width, height int) (string, error)
	// SquarePNG crops a PNG, JPEG, GIF or WebP image to its centred square
	// and scales it to a size×size PNG.
	SquarePNG(ctx context.Context, data []byte, size int) ([]byte, error)
}

// maxSourceSide is the widest or tallest image SquarePNG decodes: a small
// file can still claim dimensions that take gigabytes to decode.
const maxSourceSide = 4096

type service struct{}

func New() Service {
	return &service{}
}

func (s *service) ResizeBase64(dataBase64 string, width, height int) (string, error) {
	if !strings.HasPrefix(dataBase64, "data:image/") {
		return "", fmt.Errorf("invalid image format: missing data:image/ prefix")
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

func (s *service) SquarePNG(ctx context.Context, data []byte, size int) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if cfg.Width > maxSourceSide || cfg.Height > maxSourceSide {
		return nil, fmt.Errorf("image too large: %dx%d", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	x, y := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, image.Rect(x, y, x+side, y+side), draw.Src, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}
	return buf.Bytes(), nil
}
