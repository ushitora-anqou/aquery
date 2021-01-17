# AQuery

Yet another slow query analyzer for Go.

[Description in Japanese](https://anqou.net/teqblog/2020/10/go%E7%94%A8sql%E3%82%B9%E3%83%AD%E3%83%BC%E3%82%AF%E3%82%A8%E3%83%AA%E8%A7%A3%E6%9E%90%E3%83%84%E3%83%BC%E3%83%ABaquery%E3%82%92%E6%9B%B8%E3%81%84%E3%81%A6isucon10%E3%81%AE%E6%9C%AC%E9%81%B8%E3%81%AB%E5%87%BA%E3%81%9F%E3%82%89fail%E3%81%97%E3%81%9F/).

## Install

Rewrite your code:

```go
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
# I'll use optimized https://github.com/isucon/isucon9-final as a running example.
$ go get github.com/ushitora-anqou/aquery/aquery
$ curl "http://localhost:6060/debug/aquery?seconds=60" > query.gz
$ aquery/aquery -match-ct=isutrain -short-ct -col=100 isu9f1.gz | head -20
+-------+-------+-------+--------+-------+-------+-------+-------+-------+-------+--------------+------------------------------------------------------------------------------------------------------+
| COUNT |  MIN  |  MAX  |  SUM   |  AVG  |  P1   |  P50  |  P99  |  STD  |   K   |  CALLTRACE   |                                                 DESC                                                 |
+-------+-------+-------+--------+-------+-------+-------+-------+-------+-------+--------------+------------------------------------------------------------------------------------------------------+
|  1012 | 0.000 | 0.086 | 20.498 | 0.020 | 0.000 | 0.015 | 0.071 | 0.020 | Cl,Op | main.go:1695 |                                                                                                      |
|   736 | 0.000 | 0.113 | 17.818 | 0.024 | 0.000 | 0.025 | 0.077 | 0.017 | Cl,Op | main.go:1191 |                                                                                                      |
|   708 | 0.000 | 0.076 | 11.559 | 0.016 | 0.000 | 0.008 | 0.065 | 0.018 | Cl,Op | main.go:354  |                                                                                                      |
|  1215 | 0.000 | 0.225 | 10.078 | 0.008 | 0.000 | 0.001 | 0.132 | 0.026 | Qu    | main.go:1714 | SELECT * FROM reservations WHERE date=? AND train_class=? AND train_name=? FOR UPDATE                |
|  3794 | 0.000 | 0.141 |  6.520 | 0.002 | 0.000 | 0.001 | 0.009 | 0.005 | Qu    | main.go:1828 | SELECT * FROM seat_reservations WHERE reservation_id=? FOR UPDATE                                    |
|  7797 | 0.000 | 0.031 |  5.374 | 0.001 | 0.000 | 0.001 | 0.004 | 0.001 | Qu    | main.go:687  | SELECT departure FROM train_timetable_master WHERE date=? AND train_class=? AND train_name=? AND     |
|       |       |       |        |       |       |       |       |       |       |              | station=?                                                                                            |
|  2180 | 0.001 | 0.035 |  5.329 | 0.002 | 0.001 | 0.002 | 0.009 | 0.002 | Qu    | utils.go:64  | SELECT * FROM seat_master WHERE train_class=? AND seat_class=? AND is_smoking_seat=?                 |
|   787 | 0.003 | 0.127 |  4.165 | 0.005 | 0.003 | 0.004 | 0.014 | 0.005 | Ex    | main.go:2136 | INSERT INTO `users` (`email`, `salt`, `super_secure_password`) VALUES (?, ?, ?)                      |
|  3412 | 0.000 | 0.040 |  4.073 | 0.001 | 0.000 | 0.001 | 0.017 | 0.003 | Qu    | main.go:354  | SELECT * FROM `users` WHERE `id` = ?                                                                 |
|  5026 | 0.000 | 0.032 |  3.788 | 0.001 | 0.000 | 0.001 | 0.005 | 0.001 | Qu    | main.go:704  | SELECT arrival FROM train_timetable_master WHERE date=? AND train_class=? AND train_name=? AND       |
|       |       |       |        |       |       |       |       |       |       |              | station=?                                                                                            |
|   398 | 0.000 | 0.204 |  2.987 | 0.008 | 0.000 | 0.001 | 0.132 | 0.025 | Ex    | main.go:1925 | INSERT INTO seat_reservations (reservation_id, car_number, seat_row, seat_column) VALUES (?, ?, ?,   |
|       |       |       |        |       |       |       |       |       |       |              | ?),(?, ?, ?, ?)                                                                                      |
|  2232 | 0.000 | 0.026 |  2.716 | 0.001 | 0.000 | 0.001 | 0.008 | 0.002 | Qu    | main.go:1695 | SELECT * FROM seat_master WHERE train_class=? AND car_number=? AND seat_row=? AND seat_column=? AND  |
|       |       |       |        |       |       |       |       |       |       |              | seat_class=?                                                                                         |
|  1420 | 0.000 | 0.033 |  2.364 | 0.002 | 0.000 | 0.001 | 0.021 | 0.004 | Qu    | main.go:1191 | SELECT * FROM train_master WHERE date=? AND train_class=? AND train_name=?                           |
```
