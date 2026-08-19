// Package itembank imports QTI packages through Canvas New Quizzes UI.
package itembank

import "context"

type Existing string

const (
	ExistingFail   Existing = "fail"
	ExistingAppend Existing = "append"
)

type Request struct {
	BaseURL    string
	BrowserURL string
	CourseID   string
	BankName   string
	Package    string
	OnExisting Existing
}

type Result struct {
	BankURL string
}

// Importer controls an already-authenticated browser; it never uses Canvas APIs.
type Importer interface {
	Import(context.Context, *Request) (Result, error)
}
