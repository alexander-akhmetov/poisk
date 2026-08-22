module github.com/alexander-akhmetov/poisk

go 1.27.0

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/mattn/go-sqlite3 v1.14.50
	github.com/modelcontextprotocol/go-sdk v1.7.0
	github.com/sabhiram/go-gitignore v0.0.0-20210923224102-525f6e181f06
	github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/telemetry v0.0.0-20260209163413-e7419c687ee4 // indirect
	golang.org/x/time v0.15.0 // indirect
	golang.org/x/tools v0.42.0 // indirect
	golang.org/x/tools/go/packages/packagestest v0.1.1-deprecated // indirect
	golang.org/x/vuln v1.1.4 // indirect
)

tool (
	golang.org/x/tools/cmd/deadcode
	golang.org/x/vuln/cmd/govulncheck
)
