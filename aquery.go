package aquery

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"
	proxy "github.com/shogo82148/go-sql-proxy"
)

func writeCallStack(w io.Writer) {
	var rpc [30]uintptr
	n := runtime.Callers(4, rpc[:])
	frames := runtime.CallersFrames(rpc[:n])
	for {
		frame, more := frames.Next()
		fmt.Fprintf(w, "%s\000%d\000", frame.File, frame.Line)
		if !more {
			break
		}
	}
}

func writeBasics(out io.Writer, d time.Duration) {
	fmt.Fprintf(out, "%d\000", d.Nanoseconds())
	writeCallStack(out)
	io.WriteString(out, "\000")
}

// RegisterTracer is something.
func RegisterTracer(thresholdSlowQuery time.Duration) http.Handler {
	var out io.Writer
	var mtx sync.Mutex
	var canWrite uint32
	out = nil
	canWrite = 0

	pool := &sync.Pool{
		New: func() interface{} {
			return &bytes.Buffer{}
		},
	}

	emitResult := func(src string) {
		if atomic.LoadUint32(&canWrite) == 0 {
			return
		}
		mtx.Lock()
		if out == nil {
			return
		}
		io.WriteString(out, src)
		mtx.Unlock()
	}

	// format: kind\000conn\000desc\000duration\000CSF0\000CSF1\000...CSFn\000\000
	sql.Register("mysql:trace", proxy.NewProxyContext(&mysql.MySQLDriver{}, &proxy.HooksContext{
		PreOpen: func(_ context.Context, _ string) (interface{}, error) {
			return time.Now(), nil
		},
		PostOpen: func(_ context.Context, ctx interface{}, conn *proxy.Conn, err error) error {
			if err != nil {
				return nil
			}
			d := time.Since(ctx.(time.Time))
			if d < thresholdSlowQuery {
				return nil
			}
			buf := pool.Get().(*bytes.Buffer)
			buf.Reset()

			if conn != nil {
				fmt.Fprintf(buf, "Open\000%p\000\000", conn.Conn)
			} else {
				fmt.Fprint(buf, "Open\000nil\000\000")
			}

			writeBasics(buf, d)
			emitResult(buf.String())
			pool.Put(buf)
			return nil
		},

		PreExec: func(_ context.Context, _ *proxy.Stmt, _ []driver.NamedValue) (interface{}, error) {
			return time.Now(), nil
		},
		PostExec: func(_ context.Context, ctx interface{}, stmt *proxy.Stmt, args []driver.NamedValue, _ driver.Result, err error) error {
			if err != nil {
				return nil
			}
			d := time.Since(ctx.(time.Time))
			if d < thresholdSlowQuery {
				return nil
			}
			buf := pool.Get().(*bytes.Buffer)
			buf.Reset()

			fmt.Fprintf(buf, "Exec\000%p\000%s\000", stmt.Conn.Conn, stmt.QueryString)

			writeBasics(buf, d)
			emitResult(buf.String())
			pool.Put(buf)
			return nil
		},

		PreQuery: func(_ context.Context, stmt *proxy.Stmt, args []driver.NamedValue) (interface{}, error) {
			return time.Now(), nil
		},
		PostQuery: func(_ context.Context, ctx interface{}, stmt *proxy.Stmt, args []driver.NamedValue, _ driver.Rows, err error) error {
			if err != nil {
				return nil
			}
			d := time.Since(ctx.(time.Time))
			if d < thresholdSlowQuery {
				return nil
			}
			buf := pool.Get().(*bytes.Buffer)
			buf.Reset()

			fmt.Fprintf(buf, "Query\000%p\000%s\000", stmt.Conn.Conn, stmt.QueryString)

			writeBasics(buf, d)
			emitResult(buf.String())
			pool.Put(buf)
			return nil
		},

		PreBegin: func(_ context.Context, _ *proxy.Conn) (interface{}, error) {
			return time.Now(), nil
		},
		PostBegin: func(_ context.Context, ctx interface{}, conn *proxy.Conn, err error) error {
			if err != nil {
				return nil
			}
			d := time.Since(ctx.(time.Time))
			if d < thresholdSlowQuery {
				return nil
			}
			buf := pool.Get().(*bytes.Buffer)
			buf.Reset()

			fmt.Fprintf(buf, "Begin\000%p\000\000", conn.Conn)

			writeBasics(buf, d)
			emitResult(buf.String())
			pool.Put(buf)
			return nil
		},

		PreCommit: func(_ context.Context, _ *proxy.Tx) (interface{}, error) {
			return time.Now(), nil
		},
		PostCommit: func(_ context.Context, ctx interface{}, tx *proxy.Tx, err error) error {
			if err != nil {
				return nil
			}
			d := time.Since(ctx.(time.Time))
			if d < thresholdSlowQuery {
				return nil
			}
			buf := pool.Get().(*bytes.Buffer)
			buf.Reset()

			fmt.Fprintf(buf, "Commit\000%p\000\000", tx.Conn.Conn)

			writeBasics(buf, d)
			emitResult(buf.String())
			pool.Put(buf)
			return nil
		},

		PreRollback: func(_ context.Context, _ *proxy.Tx) (interface{}, error) {
			return time.Now(), nil
		},
		PostRollback: func(_ context.Context, ctx interface{}, tx *proxy.Tx, err error) error {
			if err != nil {
				return nil
			}
			d := time.Since(ctx.(time.Time))
			if d < thresholdSlowQuery {
				return nil
			}
			buf := pool.Get().(*bytes.Buffer)
			buf.Reset()

			fmt.Fprintf(buf, "Rollback\000%p\000\000", tx.Conn.Conn)

			writeBasics(buf, d)
			emitResult(buf.String())
			pool.Put(buf)
			return nil
		},

		PreClose: func(_ context.Context, _ *proxy.Conn) (interface{}, error) {
			return time.Now(), nil
		},
		PostClose: func(_ context.Context, ctx interface{}, conn *proxy.Conn, err error) error {
			if err != nil {
				return nil
			}
			d := time.Since(ctx.(time.Time))
			if d < thresholdSlowQuery {
				return nil
			}
			buf := pool.Get().(*bytes.Buffer)
			buf.Reset()

			fmt.Fprintf(buf, "Close\000%p\000\000", conn.Conn)

			writeBasics(buf, d)
			emitResult(buf.String())
			pool.Put(buf)
			return nil
		},
	}))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zw := gzip.NewWriter(w)
		defer zw.Close()

		var seconds int
		var err error
		if s := r.URL.Query().Get("seconds"); s == "" {
			seconds = 30
		} else if seconds, err = strconv.Atoi(s); err != nil || seconds <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "bad seconds: %d: %s\n", seconds, err)
			return
		}

		atomic.StoreUint32(&canWrite, 0)
		mtx.Lock()
		out = zw
		mtx.Unlock()
		atomic.StoreUint32(&canWrite, 1)

		time.Sleep(time.Duration(seconds) * time.Second)

		atomic.StoreUint32(&canWrite, 0)
		mtx.Lock()
		out = nil
		mtx.Unlock()
	})
}
