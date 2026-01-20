module Arrgo

go 1.25.5

require (
	github.com/gorilla/sessions v1.4.0
	github.com/jackc/pgx/v5 v5.8.0
	golang.org/x/crypto v0.46.0
)

replace github.com/justbri/arrgo/shared => ../shared

require (
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/justbri/arrgo/shared v0.0.0-00010101000000-000000000000 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)
