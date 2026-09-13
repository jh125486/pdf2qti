// Package itembank imports QTI packages through Canvas New Quizzes UI.
package itembank

import "context"

// Existing controls how Import behaves when req.BankName already exists.
type Existing string

const (
	// ExistingFail makes Import return an error if the named bank exists.
	ExistingFail Existing = "fail"
	// ExistingAppend makes Import upload into the named bank if it exists.
	ExistingAppend Existing = "append"
)

// Request describes an Item Bank import: the QTI package to upload, the
// target bank, and the Canvas session to drive it with.
type Request struct {
	BaseURL           string
	BrowserURL        string
	ChromeProfileDir  string
	Username          string
	Password          string
	CourseID          string
	BankName          string
	Package           string
	ExpectedBankName  string
	ExpectedItemCount int
	OnExisting        Existing
}

// Result reports the Item Bank an Import call created or appended to.
type Result struct {
	BankURL       string
	BankID        string
	BankName      string
	QuestionCount int
}

// Importer drives Canvas's browser UI; it never uses Canvas APIs (Canvas's New
// Quiz Items API exposes Item Banks as read-only). It manages its own Chrome
// session: if BrowserURL is unset it launches headless Chrome against
// ChromeProfileDir, logging in with Username/Password on a fresh or expired
// session; BrowserURL, when set, attaches to an existing debugging session
// instead (manual escape hatch).
type Importer interface {
	Import(context.Context, *Request) (Result, error)
}

// QuizRequest describes a New Quiz to create from random questions in an
// existing Item Bank, and the Canvas session to drive it with.
type QuizRequest struct {
	BaseURL          string
	BrowserURL       string
	ChromeProfileDir string
	Username         string
	Password         string
	CourseID         string
	BankURL          string
	BankID           string
	BankName         string
	QuestionCount    int
}

// QuizResult reports the New Quiz a CreateRandomQuiz call created.
type QuizResult struct {
	QuizURL       string
	Title         string
	QuestionCount int
}

// RandomQuizCreator creates an unpublished New Quiz using random questions
// from an Item Bank through Canvas's authenticated browser UI.
type RandomQuizCreator interface {
	PreflightRandomQuiz(context.Context, *QuizRequest) error
	CreateRandomQuiz(context.Context, *QuizRequest) (QuizResult, error)
}
