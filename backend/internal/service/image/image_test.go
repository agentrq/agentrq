package image

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func createTestImageBase64(w, h int) string {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.White)
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestImageService(t *testing.T) {
	s := New()

	t.Run("ResizeValid", func(t *testing.T) {
		input := createTestImageBase64(100, 100)
		output, err := s.ResizeBase64(input, 50, 50)
		if err != nil {
			t.Fatalf("failed to resize image: %v", err)
		}
		if output == "" {
			t.Fatal("got empty output")
		}
	})

	t.Run("NotABase64Image", func(t *testing.T) {
		input := "not a base64 image"
		output, err := s.ResizeBase64(input, 50, 50)
		if err != nil {
			t.Fatal(err)
		}
		if output != input {
			t.Errorf("expected original input, got %s", output)
		}
	})

	t.Run("NotSquare", func(t *testing.T) {
		input := createTestImageBase64(100, 50)
		_, err := s.ResizeBase64(input, 50, 50)
		if err == nil {
			t.Error("expected error for non-square image, got nil")
		}
	})

	t.Run("InvalidFormat", func(t *testing.T) {
		input := "data:image/png;base64,invalid-base64"
		_, err := s.ResizeBase64(input, 50, 50)
		if err == nil {
			t.Error("expected error for invalid base64, got nil")
		}
	})
	
	t.Run("DecodeError", func(t *testing.T) {
		input := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image"))
		_, err := s.ResizeBase64(input, 50, 50)
		if err == nil {
			t.Error("expected error for invalid image data, got nil")
		}
	})
}
