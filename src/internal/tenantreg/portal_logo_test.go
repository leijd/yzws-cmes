package tenantreg

import (
	"bytes"
	"mime/multipart"
	"testing"
)

func multipartLogo(t *testing.T, field, filename string, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

func TestTenantLogoContentType(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"png", []byte("\x89PNG\r\n\x1a\nrest"), "image/png"},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0}, "image/jpeg"},
		{"gif", []byte("GIF89a...."), "image/gif"},
		{"unknown", []byte("not-an-image"), ""},
	}
	for _, c := range cases {
		if got := tenantLogoContentType(c.data); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestExtractLogoPayloadRaw(t *testing.T) {
	/* 网页版 fetch：raw body 原样返回 */
	png := []byte("\x89PNG\r\n\x1a\nbody")
	got, err := extractLogoPayload(png, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("raw payload changed: %q", got)
	}
}

func TestExtractLogoPayloadMultipart(t *testing.T) {
	/* 移动端 uni.uploadFile：从 name="file" 的 part 中取出图片本体 */
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0, 0xFF, 0xD9}
	raw, ct := multipartLogo(t, "file", "logo.jpg", jpg)
	got, err := extractLogoPayload(raw, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, jpg) {
		t.Fatalf("multipart payload mismatch: got %d bytes, want %d", len(got), len(jpg))
	}
	if typ := tenantLogoContentType(got); typ != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", typ)
	}
}

func TestExtractLogoPayloadSkipsNonFileParts(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("source", "mobile"); err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nimage-data")
	part, err := w.CreateFormFile("file", "logo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := extractLogoPayload(buf.Bytes(), w.FormDataContentType())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("wrong part selected: %q", got)
	}
}

func TestExtractLogoPayloadInvalid(t *testing.T) {
	if _, err := extractLogoPayload([]byte("--oops"), "multipart/form-data; boundary=abc"); err == nil {
		t.Fatal("broken multipart should fail")
	}
	if _, err := extractLogoPayload([]byte("whatever"), "multipart/form-data"); err == nil {
		t.Fatal("missing boundary should fail")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("source", "mobile")
	_ = w.Close()
	if _, err := extractLogoPayload(buf.Bytes(), w.FormDataContentType()); err == nil {
		t.Fatal("multipart without file should fail")
	}
}
