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
	CourseID         string `help:"Canvas course ID."                                                                   required:""`
	BankName         string `help:"Exact Item Bank name."                                                               required:""`
	Package          string `help:"QTI ZIP package."                                                                    required:""                                       type:"path"`
	BaseURL          string `default:"https://unt.instructure.com"                                                      help:"Canvas base URL."`
	BrowserURL       string `default:"http://127.0.0.1:9222"                                                            help:"Authenticated Chrome remote-debugging URL."`
	OnExisting       string `default:"fail"                                                                             enum:"fail,append"                                help:"Existing bank behavior."`
	DryRun           bool   `help:"Validate package and report action without browser changes."`
	CreateRandomQuiz int    `help:"Create a New Quiz selecting this many random questions from the imported Item Bank."`

	importer    itembank.Importer
	quizCreator itembank.RandomQuizCreator
}

func (c *ImportBankCmd) Run(ctx context.Context, _ *CLI) error { //nolint:gocyclo // validates and coordinates optional browser workflows
	if c.CreateRandomQuiz < 0 {
		return fmt.Errorf("--create-random-quiz must be positive: %d", c.CreateRandomQuiz)
	}
	if err := validateQTIPackage(c.Package); err != nil {
		return err
	}
	packageInfo := qtiPackageInfo{}
	if c.CreateRandomQuiz > 0 {
		var err error
		packageInfo, err = inspectQTIPackage(c.Package)
		if err != nil {
			return err
		}
		if c.CreateRandomQuiz > packageInfo.ItemCount {
			return fmt.Errorf("--create-random-quiz=%d exceeds package question count %d", c.CreateRandomQuiz, packageInfo.ItemCount)
		}
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
	creator := c.quizCreator
	if creator == nil {
		creator = itembank.ChromedpImporter{}
	}
	quizRequest := &itembank.QuizRequest{
		BaseURL: c.BaseURL, BrowserURL: c.BrowserURL, CourseID: c.CourseID,
		BankName: packageInfo.Title, QuestionCount: c.CreateRandomQuiz,
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
		BaseURL: c.BaseURL, BrowserURL: c.BrowserURL, CourseID: c.CourseID,
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
	if result.BankName != packageInfo.Title {
		return fmt.Errorf("imported Item Bank title = %q, want %q", result.BankName, packageInfo.Title)
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
