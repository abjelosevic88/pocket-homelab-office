package office

import (
	"embed"
	"fmt"
)

// Blank documents to start from — the same empty files Document Server
// itself uses for "new" documents.
//
//go:embed templates/blank.docx templates/blank.xlsx templates/blank.pptx
var templates embed.FS

// Blank returns an empty document of the type (docx, xlsx or pptx).
func Blank(ext string) ([]byte, error) {
	switch ext {
	case "docx", "xlsx", "pptx":
		return templates.ReadFile("templates/blank." + ext)
	}
	return nil, fmt.Errorf("no blank %s; new files are .docx, .xlsx or .pptx", ext)
}
