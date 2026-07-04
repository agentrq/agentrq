package api

import "testing"

func TestSafeAttachmentContentType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "html becomes opaque", in: "text/html", want: "application/octet-stream"},
		{name: "javascript becomes opaque", in: "application/javascript", want: "application/octet-stream"},
		{name: "png allowed", in: "image/png", want: "image/png"},
		{name: "parameters stripped", in: "text/plain; charset=utf-8", want: "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeAttachmentContentType(tt.in); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestIsInlineAttachmentType(t *testing.T) {
	if isInlineAttachmentType("application/octet-stream") {
		t.Fatal("opaque attachments must not be inline")
	}
	if !isInlineAttachmentType("image/png") {
		t.Fatal("safe image type should be inline")
	}
}
