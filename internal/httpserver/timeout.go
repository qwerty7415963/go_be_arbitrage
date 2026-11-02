package httpserver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout returns a Gin middleware that aborts the request with 504 if the
// handler does not complete within the given duration. Paths ending in /ws or
// equal to /market/subscribe are skipped (WebSocket/SSE must not be wrapped).
// A buffered writer mirrors http.TimeoutHandler to avoid double-write races.
func Timeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasSuffix(path, "/ws") || path == "/market/subscribe" {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		original := c.Writer
		tw := &timeoutWriter{ResponseWriter: original, h: make(http.Header)}
		c.Writer = tw

		done := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					tw.mu.Lock()
					tw.err = fmt.Errorf("panic: %v", r)
					tw.mu.Unlock()
				}
				close(done)
			}()
			c.Next()
		}()

		select {
		case <-done:
			tw.mu.Lock()
			code := tw.code
			if code == 0 {
				code = http.StatusOK
			}
			hdr := tw.h
			body := append([]byte(nil), tw.buf.Bytes()...)
			tw.mu.Unlock()

			for k, vals := range hdr {
				for _, v := range vals {
					original.Header().Add(k, v)
				}
			}
			original.WriteHeader(code)
			if len(body) > 0 {
				_, _ = original.Write(body)
			}
		case <-ctx.Done():
			tw.mu.Lock()
			tw.timedOut = true
			wrote := tw.wroteHeader
			tw.mu.Unlock()
			if !wrote {
				original.Header().Set("Content-Type", "application/json")
				original.WriteHeader(http.StatusGatewayTimeout)
				_, _ = original.Write([]byte(`{"success":false,"error":{"code":"GATEWAY_TIMEOUT","message":"request timed out"}}`))
			}
		}
	}
}

type timeoutWriter struct {
	gin.ResponseWriter
	mu          sync.Mutex
	h           http.Header
	buf         bytes.Buffer
	code        int
	wroteHeader bool
	timedOut    bool
	err         error
}

func (tw *timeoutWriter) Header() http.Header { return tw.h }

func (tw *timeoutWriter) Status() int {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.code == 0 {
		return http.StatusOK
	}
	return tw.code
}

func (tw *timeoutWriter) Size() int { return tw.buf.Len() }

func (tw *timeoutWriter) Written() bool {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	return tw.wroteHeader
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.wroteHeader {
		return
	}
	tw.code = code
	tw.wroteHeader = true
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if !tw.wroteHeader {
		tw.code = http.StatusOK
		tw.wroteHeader = true
	}
	return tw.buf.Write(b)
}

func (tw *timeoutWriter) WriteString(s string) (int, error) {
	return tw.Write([]byte(s))
}

func (tw *timeoutWriter) WriteHeaderNow() {
	// buffered: no-op until completion
}
