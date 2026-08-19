package commands

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jh125486/pdf2qti/internal/itembank"
)

type ImportBankCmd struct {
	CourseID   string `help:"Canvas course ID."                                           required:""`
	BankName   string `help:"Exact Item Bank name."                                       required:""`
	Package    string `help:"QTI ZIP package."                                            required:""                                       type:"path"`
	BaseURL    string `default:"https://unt.instructure.com"                              help:"Canvas base URL."`
	BrowserURL string `default:"http://127.0.0.1:9222"                                    help:"Authenticated Chrome remote-debugging URL."`
	OnExisting string `default:"fail"                                                     enum:"fail,append"                                help:"Existing bank behavior."`
	DryRun     bool   `help:"Validate package and report action without browser changes."`
}

var newItemBankImporter = func() itembank.Importer { return itembank.ChromedpImporter{} }

func (c *ImportBankCmd) Run(ctx context.Context, _ *CLI) error {
	if err := validateQTIPackage(c.Package); err != nil {
		return err
	}
	logger := loggerFrom(ctx)
	if c.DryRun {
		logger.Info("would import QTI package", "file", c.Package, "course", c.CourseID, "bank", c.BankName)
		return nil
	}
	result, err := newItemBankImporter().Import(ctx, &itembank.Request{
		BaseURL: c.BaseURL, BrowserURL: c.BrowserURL, CourseID: c.CourseID,
		BankName: c.BankName, Package: c.Package, OnExisting: itembank.Existing(c.OnExisting),
	})
	if err != nil {
		return err
	}
	logger.Info("imported QTI package", "file", c.Package, "course", c.CourseID, "bank", c.BankName, "bank_url", result.BankURL)
	return nil
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
		if f.Name == "imsmanifest.xml" {
			return nil
		}
	}
	return fmt.Errorf("package %q lacks root imsmanifest.xml", path)
}
