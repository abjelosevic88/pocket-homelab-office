package office

import (
	"path"
	"strings"
)

// Kind is Document Server's editor family for a file type.
type Kind string

const (
	Word  Kind = "word"
	Cell  Kind = "cell"
	Slide Kind = "slide"
	PDF   Kind = "pdf"
)

// Format is what Pocket Office knows about one file extension.
type Format struct {
	Ext  string `json:"ext"`
	Kind Kind   `json:"kind"`
	// Edit means Document Server edits the format natively and saves it back
	// as the same type; without it the file is opened for viewing.
	Edit bool `json:"edit"`
	// Convertible formats open for viewing and can be turned into the
	// family's editable type ("Edit a copy as .docx").
	Convertible bool   `json:"convertible"`
	Mime        string `json:"mime"`
}

var formats = map[string]Format{}

func init() {
	add := func(kind Kind, edit, conv bool, mime string, exts ...string) {
		for _, e := range exts {
			formats[e] = Format{Ext: e, Kind: kind, Edit: edit, Convertible: conv, Mime: mime}
		}
	}
	// text documents
	add(Word, true, false, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "docx")
	add(Word, true, false, "application/vnd.ms-word.document.macroenabled.12", "docm")
	add(Word, true, false, "application/vnd.openxmlformats-officedocument.wordprocessingml.template", "dotx")
	add(Word, true, false, "application/vnd.oasis.opendocument.text", "odt")
	add(Word, true, false, "application/vnd.oasis.opendocument.text-template", "ott")
	add(Word, true, false, "application/rtf", "rtf")
	add(Word, true, false, "text/plain", "txt")
	add(Word, true, false, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "docxf", "oform")
	add(Word, false, true, "application/msword", "doc", "dot")
	add(Word, false, true, "application/epub+zip", "epub")
	add(Word, false, true, "text/html", "html", "htm", "mht")
	add(Word, false, true, "application/x-fictionbook+xml", "fb2")
	add(Word, false, true, "application/vnd.oasis.opendocument.text-flat-xml", "fodt")
	add(Word, false, true, "application/oxps", "xps", "oxps")
	add(Word, false, true, "image/vnd.djvu", "djvu")
	add(Word, false, true, "text/markdown", "md")
	add(Word, false, true, "application/x-iwork-pages-sffpages", "pages")
	// spreadsheets
	add(Cell, true, false, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "xlsx")
	add(Cell, true, false, "application/vnd.ms-excel.sheet.macroenabled.12", "xlsm")
	add(Cell, true, false, "application/vnd.openxmlformats-officedocument.spreadsheetml.template", "xltx")
	add(Cell, true, false, "application/vnd.oasis.opendocument.spreadsheet", "ods")
	add(Cell, true, false, "application/vnd.oasis.opendocument.spreadsheet-template", "ots")
	add(Cell, true, false, "text/csv", "csv")
	add(Cell, false, true, "application/vnd.ms-excel", "xls", "xlt")
	add(Cell, false, true, "application/vnd.ms-excel.sheet.binary.macroenabled.12", "xlsb")
	add(Cell, false, true, "application/vnd.oasis.opendocument.spreadsheet-flat-xml", "fods")
	add(Cell, false, true, "application/x-iwork-numbers-sffnumbers", "numbers")
	// presentations
	add(Slide, true, false, "application/vnd.openxmlformats-officedocument.presentationml.presentation", "pptx")
	add(Slide, true, false, "application/vnd.ms-powerpoint.presentation.macroenabled.12", "pptm")
	add(Slide, true, false, "application/vnd.openxmlformats-officedocument.presentationml.template", "potx")
	add(Slide, true, false, "application/vnd.openxmlformats-officedocument.presentationml.slideshow", "ppsx")
	add(Slide, true, false, "application/vnd.oasis.opendocument.presentation", "odp")
	add(Slide, true, false, "application/vnd.oasis.opendocument.presentation-template", "otp")
	add(Slide, false, true, "application/vnd.ms-powerpoint", "ppt", "pps", "pot")
	add(Slide, false, true, "application/vnd.oasis.opendocument.presentation-flat-xml", "fodp")
	add(Slide, false, true, "application/x-iwork-keynote-sffkey", "key")
	// PDF: its own editor since Document Server 8 (forms, comments, drawing)
	add(PDF, true, false, "application/pdf", "pdf")
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

// EditableExt is the type a convertible file is edited as (doc → docx).
func EditableExt(kind Kind) string {
	switch kind {
	case Cell:
		return "xlsx"
	case Slide:
		return "pptx"
	case PDF:
		return "pdf"
	}
	return "docx"
}

// Mime returns the content type for a file name.
func Mime(name string) string {
	if f, ok := Lookup(name); ok {
		return f.Mime
	}
	return "application/octet-stream"
}

// ExportTargets lists what a family can be downloaded as through the
// conversion service.
func ExportTargets(kind Kind) []string {
	switch kind {
	case Cell:
		return []string{"pdf", "xlsx", "ods", "csv"}
	case Slide:
		return []string{"pdf", "pptx", "odp"}
	case PDF:
		return []string{"docx"}
	}
	return []string{"pdf", "docx", "odt", "rtf", "txt", "epub"}
}

// All returns every known format (for the UI's filters and "New" menu).
func All() []Format {
	out := make([]Format, 0, len(formats))
	for _, f := range formats {
		out = append(out, f)
	}
	return out
}
