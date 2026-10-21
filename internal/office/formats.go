package office

import (
	_ "embed"
	"encoding/json"
	"path"
	"sort"
	"strings"
)

// Kind is Document Server's editor family for a file type (its documentType).
type Kind string

const (
	Word    Kind = "word"
	Cell    Kind = "cell"
	Slide   Kind = "slide"
	PDF     Kind = "pdf"
	Diagram Kind = "diagram" // Visio, view only
)

// Format is what Pocket Office knows about one file extension. It comes from
// the format manifest Document Server ships (onlyoffice-docs-formats.json),
// so what opens, what edits and what converts is exactly what the engine says.
type Format struct {
	Ext  string `json:"ext"`
	Kind Kind   `json:"kind"`
	// Edit means Document Server edits the format and saves it back as the
	// same type ("lossy" when the type cannot hold everything the editor can,
	// e.g. txt, rtf, odt); without it the file opens for viewing.
	Edit  bool `json:"edit"`
	Lossy bool `json:"lossy,omitempty"`
	// Convertible formats open for viewing and can be turned into the
	// family's editable type ("Edit a copy as .docx").
	Convertible bool   `json:"convertible"`
	Mime        string `json:"mime"`
	// Exports are the types the conversion service turns this one into.
	Exports []string `json:"exports"`
}

//go:embed templates/onlyoffice-docs-formats.json
var manifest []byte

var formats = map[string]Format{}

func init() {
	var entries []struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Actions []string `json:"actions"`
		Convert []string `json:"convert"`
		Mime    []string `json:"mime"`
	}
	if err := json.Unmarshal(manifest, &entries); err != nil {
		panic("office: bad format manifest: " + err.Error())
	}
	has := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	for _, e := range entries {
		if e.Type == "" || !has(e.Actions, "view") {
			continue // outputs only (png, pdfa, zip…)
		}
		f := Format{Ext: e.Name, Kind: Kind(e.Type), Edit: has(e.Actions, "edit") || has(e.Actions, "lossy-edit"), Lossy: has(e.Actions, "lossy-edit") && !has(e.Actions, "edit")}
		if len(e.Mime) > 0 {
			f.Mime = e.Mime[0]
		} else {
			f.Mime = "application/octet-stream"
		}
		editable := EditableExt(f.Kind)
		f.Convertible = !f.Edit && editable != "" && has(e.Convert, editable)
		// pdf first, then the rest as the manifest lists them, no duplicates
		seen := map[string]bool{}
		for _, c := range append([]string{"pdf"}, e.Convert...) {
			if c == e.Name || seen[c] || !has(e.Convert, c) {
				continue
			}
			seen[c] = true
			f.Exports = append(f.Exports, c)
		}
		formats[e.Name] = f
	}
}

// Ext is a file's lower-case extension without the dot.
func Ext(name string) string {
	return strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
}

// Lookup returns the format of a file name, if Document Server opens it.
func Lookup(name string) (Format, bool) {
	f, ok := formats[Ext(name)]
	return f, ok
}

// Opens reports whether Document Server can show the file.
func Opens(name string) bool {
	_, ok := Lookup(name)
	return ok
}

// EditableExt is the type a convertible file is edited as (doc → docx);
// "" for families that have no editable type (diagrams).
func EditableExt(kind Kind) string {
	switch kind {
	case Word:
		return "docx"
	case Cell:
		return "xlsx"
	case Slide:
		return "pptx"
	case PDF:
		return "pdf"
	}
	return ""
}

// Mime returns the content type for a file name.
func Mime(name string) string {
	if f, ok := Lookup(name); ok {
		return f.Mime
	}
	switch Ext(name) {
	case "pdfa":
		return "application/pdf"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "bmp":
		return "image/bmp"
	case "gif":
		return "image/gif"
	case "zip":
		return "application/zip"
	}
	return "application/octet-stream"
}

// Exports lists what a file can be downloaded as through the conversion service.
func Exports(name string) []string {
	if f, ok := Lookup(name); ok {
		return append([]string(nil), f.Exports...)
	}
	return nil
}

// CanExport reports whether the conversion service turns the file into the type.
func CanExport(name, to string) bool {
	for _, e := range Exports(name) {
		if e == to {
			return true
		}
	}
	return false
}

// All returns every known format, sorted by extension (for the UI).
func All() []Format {
	out := make([]Format, 0, len(formats))
	for _, f := range formats {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ext < out[j].Ext })
	return out
}
