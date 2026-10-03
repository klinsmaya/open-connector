module github.com/klinsmaya/open-connector/gateway

go 1.26.6

require github.com/multica-ai/multica/server v0.0.0

require (
	github.com/go-resty/resty/v2 v2.17.2 // indirect
	golang.org/x/net v0.59.0 // indirect
)

replace github.com/multica-ai/multica/server => ../../multica/server
