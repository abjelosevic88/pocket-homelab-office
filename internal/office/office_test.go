package office

import "testing"

func TestJWTRoundTrip(t *testing.T) {
	tok, err := Sign("s3cret", map[string]any{"a": 1, "exp": 9999999999})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Verify("s3cret", tok)
	if err != nil || claims["a"].(float64) != 1 {
		t.Fatalf("verify: %v %v", err, claims)
	}
	if _, err := Verify("other", tok); err == nil {
		t.Fatal("wrong secret must fail")
	}
	if _, err := Verify("s3cret", tok+"x"); err == nil {
		t.Fatal("tampered token must fail")
	}
}

func TestFormats(t *testing.T) {
	for name, want := range map[string]Kind{"Report.DOCX": Word, "a/b/c.xlsx": Cell, "deck.pptx": Slide, "x.pdf": PDF, "old.doc": Word} {
		f, ok := Lookup(name)
		if !ok || f.Kind != want {
			t.Errorf("%s: %+v %v", name, f, ok)
		}
	}
	if Opens("photo.jpg") {
		t.Error("jpg must not open")
	}
	if f, _ := Lookup("old.doc"); f.Edit || !f.Convertible {
		t.Error("doc is view + convertible")
	}
	for _, ext := range []string{"docx", "xlsx", "pptx"} {
		b, err := Blank(ext)
		if err != nil || len(b) < 1000 || string(b[:2]) != "PK" {
			t.Errorf("blank %s: %v %d", ext, err, len(b))
		}
	}
}
