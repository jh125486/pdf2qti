package qti

import (
	"archive/zip"
	"fmt"
	"io"
)

const (
	qtiNamespace      = "http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1"
	xsiNamespace      = "http://www.w3.org/2001/XMLSchema-instance"
	qtiSchemaLocation = qtiNamespace + " http://www.imsglobal.org/xsd/ims_qtiasiv1p2p1.xsd"

	manifestFilename   = "imsmanifest.xml"
	assessmentFilename = "assessment.xml"
)

const manifestXML = `<?xml version="1.0" encoding="UTF-8"?>
<manifest identifier="pdf2qti" xmlns="http://www.imsglobal.org/xsd/imscp_v1p1" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="http://www.imsglobal.org/xsd/imscp_v1p1 http://www.imsglobal.org/xsd/imscp_v1p1.xsd">
  <organizations/>
  <resources>
    <resource identifier="assessment" type="imsqti_xmlv1p2p1" href="assessment.xml">
      <file href="assessment.xml"/>
    </resource>
  </resources>
</manifest>
`

// WritePackage writes a Canvas-importable QTI 1.2 content package. Its manifest
// and assessment XML are both placed at the ZIP root as Canvas requires.
func WritePackage(w io.Writer, assessmentXML []byte) error {
	zw := zip.NewWriter(w)
	if err := writeZipFile(zw, manifestFilename, []byte(manifestXML)); err != nil {
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
