module github.com/justbri/arrgo/indexer

go 1.26.1

replace github.com/justbri/arrgo/shared => ../shared

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/justbri/arrgo/shared v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.49.0
)
