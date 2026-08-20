package commands

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jh125486/pdf2qti/internal/itembank"
)

type ImportBankCmd struct {
	CourseID         string `help:"Canvas course ID."                                                                                                 required:""`
	BankName         string `help:"Exact Item Bank name."                                                                                             required:""`
	Package          string `help:"QTI ZIP package."                                                                                                  required:""                                                                                                     type:"path"`
	BaseURL          string `default:"https://unt.instructure.com"                                                                                    help:"Canvas base URL."`
	Username         string `env:"CANVAS_USERNAME"                                                                                                    help:"Canvas login username, used to log in headless Chrome if the persisted session has expired."`
	Password         string `env:"CANVAS_PASSWORD"                                                                                                    help:"Canvas login password, used to log in headless Chrome if the persisted session has expired."`
	ChromeProfileDir string `env:"PDF2QTI_CHROME_PROFILE_DIR"                                                                                         help:"Persisted headless Chrome profile directory (default: a pdf2qti directory under the OS user-config dir)."`
	BrowserURL       string `help:"Attach to an existing Chrome remote-debugging session instead of launching headless Chrome (manual escape hatch)."`
	OnExisting       string `default:"fail"                                                                                                           enum:"fail,append"                                                                                              help:"Existing bank behavior."`
	DryRun           bool   `help:"Validate package and report action without browser changes."`
	CreateRandomQuiz int    `help:"Create a New Quiz selecting this many random questions from the imported Item Bank."`

	importer    itembank.Importer
	quizCreator itembank.RandomQuizCreator
}

// defaultChromeProfileDir is where headless Chrome's Canvas session persists
// across runs when --chrome-profile-dir isn't set, so most invocations need
// no login at all.
func defaultChromeProfileDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve default Chrome profile directory: %w", err)
	}
	return filepath.Join(configDir, "pdf2qti", "chrome-profile"), nil
}

func (c *ImportBankCmd) Run(ctx context.Context, _ *CLI) error { //nolint:gocyclo // validates and coordinates optional browser workflows
	if c.CreateRandomQuiz < 0 {
		return fmt.Errorf("--create-random-quiz must be non-negative: %d", c.CreateRandomQuiz)
	}
	if err := validateQTIPackage(c.Package); err != nil {
		return err
	}
	// Inspected unconditionally: Canvas renames a New Quizzes Item Bank after
	// the imported QTI package's assessment title even when --create-random-quiz
	// isn't used, so the import step needs packageInfo.Title to detect and
	// correct that regardless of whether a random quiz is also created.
	packageInfo, err := inspectQTIPackage(c.Package)
	if err != nil {
		return err
	}
	if c.CreateRandomQuiz > packageInfo.ItemCount {
		return fmt.Errorf("--create-random-quiz=%d exceeds package question count %d", c.CreateRandomQuiz, packageInfo.ItemCount)
	}
	logger := loggerFrom(ctx)
	if c.DryRun {
		if c.CreateRandomQuiz > 0 {
			logger.Info("would import QTI package and create random quiz", "file", c.Package, "course", c.CourseID, "bank", c.BankName, "create_random_quiz", c.CreateRandomQuiz, "quiz_title", itembank.QuizTitle(packageInfo.Title))
		} else {
			logger.Info("would import QTI package", "file", c.Package, "course", c.CourseID, "bank", c.BankName)
		}
		return nil
	}
	profileDir := c.ChromeProfileDir
	if c.BrowserURL == "" && profileDir == "" {
		var err error
		profileDir, err = defaultChromeProfileDir()
		if err != nil {
			return err
		}
	}
	if c.BrowserURL == "" {
		// The profile directory persists a live Canvas session; restrict it to
		// the current user, matching the same care given to bearer tokens.
		// MkdirAll's mode only applies when it creates the directory, so a
		// pre-existing directory (e.g. left group/world-readable by another
		// tool) needs its permissions tightened explicitly too.
		if err := os.MkdirAll(profileDir, 0o700); err != nil {
			return fmt.Errorf("create Chrome profile directory %q: %w", profileDir, err)
		}
		if err := os.Chmod(profileDir, 0o700); err != nil { //nolint:gosec // 0700 is correct for a directory: execute bit required to traverse it, not a permissiveness bug
			return fmt.Errorf("restrict Chrome profile directory %q to the current user: %w", profileDir, err)
		}
	}
	creator := c.quizCreator
	if creator == nil {
		creator = itembank.ChromedpImporter{}
	}
	quizRequest := &itembank.QuizRequest{
		BaseURL: c.BaseURL, BrowserURL: c.BrowserURL, ChromeProfileDir: profileDir,
		Username: c.Username, Password: c.Password, CourseID: c.CourseID,
		BankName: c.BankName, QuestionCount: c.CreateRandomQuiz,
	}
	if c.CreateRandomQuiz > 0 {
		if err := creator.PreflightRandomQuiz(ctx, quizRequest); err != nil {
			return fmt.Errorf("preflight random quiz: %w", err)
		}
	}
	importer := c.importer
	if importer == nil {
		importer = itembank.ChromedpImporter{}
	}
	result, err := importer.Import(ctx, &itembank.Request{
		BaseURL: c.BaseURL, BrowserURL: c.BrowserURL, ChromeProfileDir: profileDir,
		Username: c.Username, Password: c.Password, CourseID: c.CourseID,
		BankName: c.BankName, Package: c.Package, ExpectedBankName: packageInfo.Title,
		ExpectedItemCount: packageInfo.ItemCount,
		OnExisting:        itembank.Existing(c.OnExisting),
	})
	if err != nil {
		return err
	}
	logger.Info("imported QTI package", "file", c.Package, "course", c.CourseID, "bank", c.BankName, "bank_url", result.BankURL)
	if c.CreateRandomQuiz == 0 {
		return nil
	}
	if result.BankName != c.BankName {
		return fmt.Errorf("imported Item Bank title = %q, want %q", result.BankName, c.BankName)
	}
	if result.QuestionCount != packageInfo.ItemCount {
		return fmt.Errorf("imported Item Bank question count = %d, want %d", result.QuestionCount, packageInfo.ItemCount)
	}
	quizRequest.BankURL = result.BankURL
	quizRequest.BankID = result.BankID
	quizRequest.BankName = result.BankName
	quiz, err := creator.CreateRandomQuiz(ctx, quizRequest)
	if err != nil {
		return fmt.Errorf("item bank imported at %s, but create random quiz: %w", result.BankURL, err)
	}
	logger.Info("created random quiz", "course", c.CourseID, "bank", result.BankName, "quiz", quiz.Title, "quiz_url", quiz.QuizURL, "questions", quiz.QuestionCount)
	return nil
}

type qtiPackageInfo struct {
	Title     string
	ItemCount int
}

type qtiManifest struct {
	Resources []struct {
		Type string `xml:"type,attr"`
		File struct {
			Href string `xml:"href,attr"`
		} `xml:"file"`
	} `xml:"resources>resource"`
}

const qtiManifestFilename = "imsmanifest.xml"

func inspectQTIPackage(path string) (qtiPackageInfo, error) { //nolint:gocyclo // fail-closed ZIP and XML validation
	zr, err := zip.OpenReader(path)
	if err != nil {
		return qtiPackageInfo{}, fmt.Errorf("open package %q: %w", path, err)
	}
	defer zr.Close()
	files := make(map[string]*zip.File, len(zr.File))
	for _, file := range zr.File {
		files[file.Name] = file
	}
	manifestFile := files[qtiManifestFilename]
	if manifestFile == nil {
		return qtiPackageInfo{}, fmt.Errorf("package %q lacks root %s", path, qtiManifestFilename)
	}
	manifestReader, err := manifestFile.Open()
	if err != nil {
		return qtiPackageInfo{}, fmt.Errorf("read package manifest: %w", err)
	}
	var manifest qtiManifest
	decodeErr := xml.NewDecoder(manifestReader).Decode(&manifest)
	closeErr := manifestReader.Close()
	if decodeErr != nil {
		return qtiPackageInfo{}, fmt.Errorf("parse package manifest: %w", decodeErr)
	}
	if closeErr != nil {
		return qtiPackageInfo{}, fmt.Errorf("close package manifest: %w", closeErr)
	}
	assessmentName := ""
	for _, resource := range manifest.Resources {
		if resource.Type != "imsqti_xmlv1p2" {
			continue
		}
		if assessmentName != "" {
			return qtiPackageInfo{}, fmt.Errorf("package %q has multiple QTI 1.2 resources", path)
		}
		assessmentName = resource.File.Href
	}
	if assessmentName == "" {
		return qtiPackageInfo{}, fmt.Errorf("package %q has no QTI 1.2 resource", path)
	}
	if filepath.Base(assessmentName) != assessmentName || filepath.Ext(assessmentName) != ".xml" {
		return qtiPackageInfo{}, fmt.Errorf("QTI assessment must be a root-level .xml file: %q", assessmentName)
	}
	assessmentFile := files[assessmentName]
	if assessmentFile == nil {
		return qtiPackageInfo{}, fmt.Errorf("package %q lacks referenced assessment %q", path, assessmentName)
	}
	assessmentReader, err := assessmentFile.Open()
	if err != nil {
		return qtiPackageInfo{}, fmt.Errorf("read package entry %q: %w", assessmentName, err)
	}
	decoder := xml.NewDecoder(assessmentReader)
	info := qtiPackageInfo{}
	for {
		token, tokenErr := decoder.Token()
		if errors.Is(tokenErr, io.EOF) {
			break
		}
		if tokenErr != nil {
			_ = assessmentReader.Close()
			return qtiPackageInfo{}, fmt.Errorf("parse package entry %q: %w", assessmentName, tokenErr)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "assessment":
			for _, attr := range start.Attr {
				if attr.Name.Local == "title" {
					info.Title = strings.TrimSpace(attr.Value)
				}
			}
		case "item":
			info.ItemCount++
		}
	}
	if err := assessmentReader.Close(); err != nil {
		return qtiPackageInfo{}, fmt.Errorf("close package entry %q: %w", assessmentName, err)
	}
	if info.Title == "" {
		return qtiPackageInfo{}, fmt.Errorf("package %q QTI assessment title is empty", path)
	}
	if info.ItemCount == 0 {
		return qtiPackageInfo{}, fmt.Errorf("package %q contains no QTI question items", path)
	}
	return info, nil
}

func validateQTIPackage(path string) error {
	if filepath.Ext(path) != ".zip" {
		return fmt.Errorf("package must be a .zip file: %q", path)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("read package %q: %w", path, err)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("open package %q: %w", path, err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == qtiManifestFilename {
			return nil
		}
	}
	return fmt.Errorf("package %q lacks root %s", path, qtiManifestFilename)
}
