module github.com/echotools/nevr-agent/v4

go 1.26.0

require (
	buf.build/gen/go/echotools/nevr-api/protocolbuffers/go v1.36.12-20260907104457-638a4669f605.2
	github.com/echotools/tape/v4 v4.0.0
	github.com/google/go-cmp v0.7.0
	github.com/joho/godotenv v1.5.1
	github.com/klauspost/compress v1.19.2
	github.com/schollz/progressbar/v3 v3.19.1
	github.com/spf13/cobra v1.10.2
	go.uber.org/zap v1.28.0
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

require (
	github.com/gorilla/websocket v1.5.3
	go.uber.org/multierr v1.11.0 // indirect
)

// The /v4 module path IS on tape's origin/main now (5a40180), but no tag resolves
// to it: v4.0.0 was cut on 5916867, whose go.mod still declares the v1 path, so
// both `@v4.0.0` and `@latest` fail (Go selects the highest TAG, and that is the
// broken one). A pseudo-version does resolve --
// v4.0.1-0.20260826154530-5a4018052364 -- so this replace is now a choice rather
// than a necessity. It is kept until the owner rules on cutting v4.0.1, and it
// lives here rather than in a gitignored go.work so the repo builds without
// local-only state. nevr-stream's go.mod carries the same note and the same
// exit condition; the two should move together.
replace github.com/echotools/tape/v4 => ../tape
