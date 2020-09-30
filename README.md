# AQuery

Yet another slow query analyzer for Go.

## Install

Rewrite your code:

```
import "github.com/ushitora-anqou/aquery"

func main() {
	thresholdSlowQuery, err := time.ParseDuration("0ms")
	handler := aquery.RegisterTracer(thresholdSlowQuery)

	http.DefaultServeMux.Handle("/debug/go-query", handler)
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	db, err := sql.Open("mysql:trace", "data source")
	if err != nil {
		log.Fatalf("Could not open db: %s", err.Error())
	}

	// Use db
}
```

Install aquery command, get the profile, and analyze it:

```
$ go get github.com/ushitora-anqou/aquery/aquery
$ curl "http://localhost:6060/debug/go-query?seconds=3" > query.gz
$ aquery -match-callstack="main" query.gz
```
