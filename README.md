# AQuery

Yet another slow query analyzer for Go.

## Install

Rewrite your code:

```
import "github.com/ushitora-anqou/aquery"

func main() {
	thresholdSlowQuery, err := time.ParseDuration("0ms")
	handler := aquery.RegisterTracer(thresholdSlowQuery)

	http.DefaultServeMux.Handle("/debug/aquery", handler)
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
$ curl "http://localhost:6060/debug/aquery?seconds=60" > query.gz
$ aquery -sort=sum -match-callstack="main" query.gz | head -20
+-------+-------+-------+--------+-------+----+--------------------------------+-------------------------------------------------+
| COUNT |  MIN  |  MAX  |  SUM   |  AVG  | K  |              DESC              |                    CALLTRACE                    |
+-------+-------+-------+--------+-------+----+--------------------------------+-------------------------------------------------+
|   656 | 0.006 | 0.086 | 20.471 | 0.031 | Op |                                | 00:/home/isucon/isutrain/webapp/go/main.go:1695 |
|   614 | 0.006 | 0.113 | 17.810 | 0.029 | Op |                                | 00:/home/isucon/isutrain/webapp/go/main.go:1191 |
|  1215 | 0.000 | 0.225 | 10.078 | 0.008 | Qu | SELECT * FROM reservations     | 00:/home/isucon/isutrain/webapp/go/main.go:1714 |
|       |       |       |        |       |    | WHERE date=? AND train_class=? |                                                 |
|       |       |       |        |       |    | AND train_name=? FOR UPDATE    |                                                 |
|   190 | 0.007 | 0.073 |  6.532 | 0.034 | Op |                                | 00:/home/isucon/isutrain/webapp/go/main.go:354  |
|       |       |       |        |       |    |                                | 01:/home/isucon/isutrain/webapp/go/main.go:1386 |
|  3794 | 0.000 | 0.141 |  6.520 | 0.002 | Qu | SELECT * FROM                  | 00:/home/isucon/isutrain/webapp/go/main.go:1828 |
|       |       |       |        |       |    | seat_reservations WHERE        |                                                 |
|       |       |       |        |       |    | reservation_id=? FOR UPDATE    |                                                 |
|  7797 | 0.000 | 0.031 |  5.374 | 0.001 | Qu | SELECT departure FROM          | 00:/home/isucon/isutrain/webapp/go/main.go:687  |
|       |       |       |        |       |    | train_timetable_master WHERE   |                                                 |
|       |       |       |        |       |    | date=? AND train_class=? AND   |                                                 |
|       |       |       |        |       |    | train_name=? AND station=?     |                                                 |
|   787 | 0.003 | 0.127 |  4.165 | 0.005 | Ex | INSERT INTO `users`            | 00:/home/isucon/isutrain/webapp/go/main.go:2136 |
|       |       |       |        |       |    | (`email`, `salt`,              |                                                 |
|       |       |       |        |       |    | `super_secure_password`)       |                                                 |
```
