package storage

import (
	"context"
	"errors"
	"testing"
)

func TestClean(t *testing.T) {
	for in, want := range map[string]string{"": "", "/a/b/": "a/b", "a//b": "a/b", "./a": "a", `a\b`: "a/b"} {
		got, err := Clean(in)
		if err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Clean("a/../b"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("Clean(a/../b) should be refused, got %v", err)
	}
}

func TestLocalRoundTrip(t *testing.T) {
	ctx := context.Background()
	l := NewLocal(t.TempDir()+"/files", "builtin", "My files")
	if err := l.Test(ctx); err != nil {
		t.Fatal(err)
	}
	e, err := l.Create(ctx, "Reports/q1.docx", []byte("one"))
	if err != nil || e.Size != 3 || e.Name != "q1.docx" {
		t.Fatalf("create: %v %+v", err, e)
	}
	if _, err := l.Create(ctx, "Reports/q1.docx", []byte("two")); !errors.Is(err, ErrExists) {
		t.Fatalf("second create should be ErrExists, got %v", err)
	}
	es, err := l.ListDir(ctx, "")
	if err != nil || len(es) != 1 || !es[0].IsDir || es[0].Name != "Reports" {
		t.Fatalf("list root: %v %+v", err, es)
	}
	es, _ = l.ListDir(ctx, "Reports")
	if len(es) != 1 || es[0].Path != "Reports/q1.docx" {
		t.Fatalf("list Reports: %+v", es)
	}
	if _, err := l.Write(ctx, "Reports/q1.docx", []byte("three"), "stale"); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("stale write should be ErrPrecondition, got %v", err)
	}
	e2, err := l.Write(ctx, "Reports/q1.docx", []byte("three"), e.ETag)
	if err != nil || e2.ETag == e.ETag {
		t.Fatalf("write: %v %+v", err, e2)
	}
	data, _, err := l.Read(ctx, "Reports/q1.docx")
	if err != nil || string(data) != "three" {
		t.Fatalf("read: %v %q", err, data)
	}
	if _, err := l.Move(ctx, "Reports/q1.docx", "Reports/q2.docx"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Stat(ctx, "Reports/q1.docx"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old name should be gone, got %v", err)
	}
	if err := l.Delete(ctx, "Reports"); err != nil {
		t.Fatal(err)
	}
	if es, _ := l.ListDir(ctx, ""); len(es) != 0 {
		t.Fatalf("root should be empty, got %+v", es)
	}
	if _, _, err := l.Read(ctx, "../etc/passwd"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("escape should be refused, got %v", err)
	}
}
