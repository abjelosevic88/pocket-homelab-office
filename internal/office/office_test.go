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

func TestFormatsFromManifest(t *testing.T) {
	for name, want := range map[string]Kind{"Report.DOCX": Word, "a/b/c.xlsx": Cell, "deck.pptx": Slide, "x.pdf": PDF, "old.doc": Word, "flow.vsdx": Diagram, "macro.dotm": Word, "t.xltm": Cell, "show.ppsm": Slide} {
		f, ok := Lookup(name)
		if !ok || f.Kind != want {
			t.Errorf("%s: %+v %v", name, f, ok)
		}
	}
	if Opens("photo.jpg") || Opens("archive.zip") || Opens("x.pdfa") {
		t.Error("outputs-only types must not open")
	}
	for _, ext := range []string{"docx", "dotm", "xlsx", "xltm", "pptx", "ppsm", "pdf", "odt", "txt"} {
		if f, _ := Lookup("f." + ext); !f.Edit {
			t.Errorf("%s should edit", ext)
		}
	}
	if f, _ := Lookup("f.txt"); !f.Lossy {
		t.Error("txt is a lossy edit")
	}
	if f, _ := Lookup("old.doc"); f.Edit || !f.Convertible {
		t.Errorf("doc is view + convertible: %+v", f)
	}
	if f, _ := Lookup("flow.vsdx"); f.Edit || f.Convertible || EditableExt(f.Kind) != "" {
		t.Errorf("vsdx is view only: %+v", f)
	}
	ex := Exports("a.docx")
	if len(ex) < 10 || ex[0] != "pdf" || !CanExport("a.docx", "pdfa") || !CanExport("a.docx", "png") || CanExport("a.docx", "docx") {
		t.Errorf("docx exports: %v", ex)
	}
	if !CanExport("a.xlsx", "csv") || !CanExport("a.pptx", "pdf") || !CanExport("flow.vsdx", "pdf") || !CanExport("a.pdf", "docx") {
		t.Error("expected exports missing")
	}
	if Mime("a.docx") != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || Mime("x.pdfa") != "application/pdf" {
		t.Error("mime")
	}
	if n := len(All()); n < 60 {
		t.Errorf("only %d formats", n)
	}
	for _, ext := range []string{"docx", "xlsx", "pptx"} {
		b, err := Blank(ext)
		if err != nil || len(b) < 1000 || string(b[:2]) != "PK" {
			t.Errorf("blank %s: %v %d", ext, err, len(b))
		}
	}
}
