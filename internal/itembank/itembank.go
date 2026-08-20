// Package itembank imports QTI packages through Canvas New Quizzes UI.
package itembank

import "context"

type Existing string

const (
	ExistingFail   Existing = "fail"
	ExistingAppend Existing = "append"
)

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
