package router

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// apiGzipExcludedStreamPaths mirrors the exclusion list installed by
// SetApiRouter. Keep the two in sync: every /api route that flushes an SSE
// transcript belongs here.
var apiGzipExcludedStreamPaths = []string{
	"/api/channel/ollama/pull/stream",
	"/api/channel_monitor/probe_stream",
}

// newAPIGzipStreamServer serves one flush-per-event SSE handler behind the same
// gzip middleware configuration SetApiRouter installs. The handler emits the
// first event, then blocks until released, so a reader that can see that event
// proves the middleware forwarded it instead of buffering it until completion.
func newAPIGzipStreamServer(t *testing.T, path string, release <-chan struct{}) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(gzip.Gzip(gzip.DefaultCompression, gzip.WithExcludedPaths(apiGzipExcludedStreamPaths)))
	engine.POST(path, func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream; charset=utf-8")
		_, err := c.Writer.WriteString("event: start\ndata: {}\n\n")
		require.NoError(t, err)
		c.Writer.Flush()
		<-release
		_, err = c.Writer.WriteString("event: end\ndata: {}\n\n")
		require.NoError(t, err)
		c.Writer.Flush()
	})

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return server
}

// readFirstSSEEvent reads until the blank line that terminates the first SSE
// event, reporting whether it arrived before the deadline. A gzip-buffered
// response yields nothing here because the compressor holds every flushed byte
// until the handler returns.
func readFirstSSEEvent(t *testing.T, body *bufio.Reader) (string, bool) {
	t.Helper()
	type readResult struct {
		line string
		err  error
	}
	lines := make(chan readResult, 1)
	go func() {
		line, err := body.ReadString('\n')
		lines <- readResult{line: line, err: err}
	}()

	select {
	case result := <-lines:
		require.NoError(t, result.err)
		return result.line, true
	case <-time.After(5 * time.Second):
		return "", false
	}
}

// The manual channel-monitor probe streams its console transcript over SSE.
// gin-contrib/gzip's writer has no Flush of its own, so with compression active
// c.Writer.Flush() reaches the socket while the compressor keeps holding the
// bytes: the administrator would see the whole transcript appear at once when
// the probe finished instead of watching it live. Every excluded streaming path
// must therefore stay readable mid-request even when the client offers gzip and
// omits an SSE Accept header.
func TestAPIGzipExcludesStreamingPathsSoEventsArriveBeforeCompletion(t *testing.T) {
	for _, path := range apiGzipExcludedStreamPaths {
		t.Run(path, func(t *testing.T) {
			release := make(chan struct{})
			defer close(release)
			server := newAPIGzipStreamServer(t, path, release)

			request, err := http.NewRequest(http.MethodPost, server.URL+path, nil)
			require.NoError(t, err)
			// A browser fetch always offers gzip; the bug only shows when the
			// middleware accepts that offer for a streaming response.
			request.Header.Set("Accept-Encoding", "gzip")

			response, err := http.DefaultTransport.RoundTrip(request)
			require.NoError(t, err)
			defer response.Body.Close()

			require.Empty(t, response.Header.Get("Content-Encoding"),
				"streaming path must not be gzip encoded")

			line, ok := readFirstSSEEvent(t, bufio.NewReader(response.Body))
			require.True(t, ok, "first SSE event never arrived; response is being buffered")
			require.Equal(t, "event: start\n", line)
		})
	}
}

// A client that declares Accept: text/event-stream must stream even on a path
// that is not in the exclusion list, since that is the header the frontend
// probe client sends and the middleware's own bypass depends on it.
func TestAPIGzipSkipsCompressionForSSEAcceptHeader(t *testing.T) {
	const path = "/api/some/other/stream"
	release := make(chan struct{})
	defer close(release)
	server := newAPIGzipStreamServer(t, path, release)

	request, err := http.NewRequest(http.MethodPost, server.URL+path, nil)
	require.NoError(t, err)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Accept", "text/event-stream")

	response, err := http.DefaultTransport.RoundTrip(request)
	require.NoError(t, err)
	defer response.Body.Close()

	require.Empty(t, response.Header.Get("Content-Encoding"))
	line, ok := readFirstSSEEvent(t, bufio.NewReader(response.Body))
	require.True(t, ok, "first SSE event never arrived; response is being buffered")
	require.Equal(t, "event: start\n", line)
}
