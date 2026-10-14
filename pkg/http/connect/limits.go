package connect

import (
	"net/http"
	"time"
)

// envelopeSlack lets connect's own ReadMaxBytes check (which reports
// ResourceExhausted) fire before the hard MaxBytesReader stop.
const envelopeSlack = 1024

// limitsMiddleware enforces limits before connect reads the body: GET to a
// procedure is refused unless enabled, and unary procedures get a body cap and
// a read deadline. Streams keep unbounded bodies and durations; their messages
// are still capped by connect's per-message ReadMaxBytes. procedures maps each
// procedure path to whether it is unary; other paths pass untouched.
func (c *Config) limitsMiddleware(procedures map[string]bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unary, isProcedure := procedures[r.URL.Path]
		if isProcedure && r.Method == http.MethodGet && !c.AllowGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		if unary {
			if c.ReadMaxBytes > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, int64(c.ReadMaxBytes)+envelopeSlack)
			}
			if c.ReadTimeout > 0 {
				_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(c.ReadTimeout))
			}
		}

		next.ServeHTTP(w, r)
	})
}
