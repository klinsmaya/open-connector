module github.com/klinsmaya/open-connector/gateway

go 1.26.6

require (
	github.com/jackc/pgx/v5 v5.11.0
	github.com/multica-ai/multica/server v0.0.0
)

require (
	github.com/go-resty/resty/v2 v2.17.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace github.com/multica-ai/multica/server => ../../multica/server
