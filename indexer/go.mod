module github.com/justbri/arrgo/indexer

go 1.25.5

replace github.com/justbri/arrgo/shared => ../shared

replace Arrgo => ../server

require (
	Arrgo v0.0.0-00010101000000-000000000000
	github.com/justbri/arrgo/shared v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.49.0
)

require (
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/gorilla/sessions v1.4.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.8.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/text v0.33.0 // indirect
)
