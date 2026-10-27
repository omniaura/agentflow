module github.com/omniaura/agentflow/adapters/afthreadpg

go 1.27

require (
	github.com/jackc/pgx/v5 v5.11.0
	github.com/omniaura/agentflow v0.13.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/omniaura/go-kit/errs v0.0.0-20261024021028-d0a008fba30c // indirect
	github.com/rs/xid v1.6.0 // indirect
	github.com/rs/zerolog v1.34.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace github.com/omniaura/agentflow => ../..
