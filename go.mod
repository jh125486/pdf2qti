module github.com/jh125486/pdf2qti

go 1.26

require github.com/alecthomas/kong v1.15.0

require (
	golang.org/x/mod v0.30.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/telemetry v0.0.0-20251111182119-bc8e575c7b54 // indirect
	golang.org/x/tools v0.39.1-0.20260109155911-b69ac100ecb7 // indirect
	golang.org/x/tools/gopls v0.21.1 // indirect
	golang.org/x/vuln v1.1.4 // indirect
)

tool (
	golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize
	golang.org/x/vuln/cmd/govulncheck
)
