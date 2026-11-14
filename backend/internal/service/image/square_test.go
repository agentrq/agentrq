// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package image

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// banded is a w×h PNG, red in its left third, blue in its middle third and
// green in its right third.
func banded(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		c := color.RGBA{0, 0, 255, 255}
		switch {
		case x < w/3:
			c = color.RGBA{255, 0, 0, 255}
		case x >= 2*w/3:
			c = color.RGBA{0, 255, 0, 255}
		}
		for y := 0; y < h; y++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSquarePNG(t *testing.T) {
	s := New()
	ctx := context.Background()

	t.Run("crops a wide picture to its centre and scales it to a PNG of the size asked", func(t *testing.T) {
		out, err := s.SquarePNG(ctx, banded(t, 300, 100), 32)
		if err != nil {
			t.Fatalf("SquarePNG returned %v, want a picture", err)
		}
		img, format, err := image.Decode(bytes.NewReader(out))
		if err != nil || format != "png" {
			t.Fatalf("the result decodes as %q, %v, want a PNG", format, err)
		}
		if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
			t.Fatalf("the result is %dx%d, want 32x32", b.Dx(), b.Dy())
		}
		// The centred square is the blue band alone.
		for _, x := range []int{0, 16, 31} {
			r, g, b, _ := img.At(x, 16).RGBA()
			if r>>8 > 8 || g>>8 > 8 || b>>8 < 247 {
				t.Errorf("pixel (%d,16) is %d,%d,%d, want blue: the crop is off centre", x, r>>8, g>>8, b>>8)
			}
		}
	})

	t.Run("reads a JPEG", func(t *testing.T) {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 60)), nil); err != nil {
			t.Fatal(err)
		}
		out, err := s.SquarePNG(ctx, buf.Bytes(), 32)
		if err != nil || !bytes.HasPrefix(out, []byte("\x89PNG")) {
			t.Fatalf("SquarePNG returned %v, want a PNG", err)
		}
	})

	t.Run("refuses what is not a picture", func(t *testing.T) {
		if _, err := s.SquarePNG(ctx, []byte("<html>"), 32); err == nil || !strings.Contains(err.Error(), "decode image") {
			t.Errorf("SquarePNG returned %v, want a decode error", err)
		}
	})

	t.Run("refuses a picture cut short after its header", func(t *testing.T) {
		data := banded(t, 30, 30)
		if _, err := s.SquarePNG(ctx, data[:len(data)/2], 32); err == nil || !strings.Contains(err.Error(), "decode image") {
			t.Errorf("SquarePNG returned %v, want a decode error", err)
		}
	})

	t.Run("refuses a picture too large to decode safely", func(t *testing.T) {
		if _, err := s.SquarePNG(ctx, banded(t, maxSourceSide+1, 1), 32); err == nil || !strings.Contains(err.Error(), "too large") {
			t.Errorf("SquarePNG returned %v, want a refusal", err)
		}
	})

	t.Run("reports a size no PNG can have", func(t *testing.T) {
		if _, err := s.SquarePNG(ctx, banded(t, 30, 30), 0); err == nil || !strings.Contains(err.Error(), "encode image") {
			t.Errorf("SquarePNG returned %v, want an encode error", err)
		}
	})
}
