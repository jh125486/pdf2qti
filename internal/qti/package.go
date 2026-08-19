package qti

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

const (
	qtiNamespace      = "http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1"
	xsiNamespace      = "http://www.w3.org/2001/XMLSchema-instance"
	qtiSchemaLocation = qtiNamespace + " http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1.xsd"

	manifestFilename = "imsmanifest.xml"
)

// WritePackage writes a Canvas-importable QTI 1.2 content package. Its manifest
// and assessment XML are both placed at the ZIP root as Canvas requires.
func WritePackage(w io.Writer, assessmentFilename string, assessmentXML []byte) error {
	if filepath.Base(assessmentFilename) != assessmentFilename || !strings.HasSuffix(assessmentFilename, ".xml") {
		return fmt.Errorf("assessment filename must be a root-level .xml file: %q", assessmentFilename)
	}
	manifest, err := buildManifest(assessmentFilename)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	if err := writeZipFile(zw, manifestFilename, manifest); err != nil {
		return err
	}
	if err := writeZipFile(zw, assessmentFilename, assessmentXML); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close QTI package: %w", err)
	}
	return nil
}

func buildManifest(assessmentFilename string) ([]byte, error) {
	var escaped strings.Builder
	if err := xml.EscapeText(&escaped, []byte(strings.TrimSuffix(assessmentFilename, ".xml"))); err != nil {
		return nil, fmt.Errorf("escape assessment identifier: %w", err)
	}
	assessmentID := escaped.String()
	escaped.Reset()
	if err := xml.EscapeText(&escaped, []byte(assessmentFilename)); err != nil {
		return nil, fmt.Errorf("escape assessment filename: %w", err)
	}
	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<manifest identifier="MANIFEST-%[1]s" xmlns="http://www.imsglobal.org/xsd/imscp_v1p1" xmlns:imsmd="http://www.imsglobal.org/xsd/imsmd_v1p2" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://www.imsglobal.org/xsd/imscp_v1p1 http://www.imsglobal.org/xsd/imscp_v1p1.xsd http://www.imsglobal.org/xsd/imsmd_v1p2 http://www.imsglobal.org/xsd/imsmd_v1p2p2.xsd">
  <metadata><schema>IMS Content</schema><schemaversion>1.1.3</schemaversion></metadata>
  <organizations/>
  <resources><resource identifier="%[1]s_res" type="imsqti_xmlv1p2"><file href="%[2]s"/></resource></resources>
</manifest>`, assessmentID, escaped.String())), nil
}

func writeZipFile(zw *zip.Writer, name string, content []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create ZIP entry %q: %w", name, err)
	}
	if _, err := w.Write(content); err != nil {
		return fmt.Errorf("write ZIP entry %q: %w", name, err)
	}
	return nil
}
