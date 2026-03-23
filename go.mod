module github.com/alexander-akhmetov/poisk

go 1.26.1

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/mattn/go-sqlite3 v1.14.34
	github.com/sabhiram/go-gitignore v0.0.0-20210923224102-525f6e181f06
	github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
)

require (
	github.com/google/go-cmp v0.7.0 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/telemetry v0.0.0-20260209163413-e7419c687ee4 // indirect
	golang.org/x/tools v0.42.0 // indirect
	golang.org/x/tools/go/packages/packagestest v0.1.1-deprecated // indirect
	golang.org/x/vuln v1.1.4 // indirect
)

tool (
	golang.org/x/tools/cmd/deadcode
	golang.org/x/vuln/cmd/govulncheck
)
