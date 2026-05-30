package prof_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alex-cos/prof"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cpuProfMu serializes tests that use the global CPU profiler.
var cpuProfMu sync.Mutex

// ---------------------------------------------------------------------------
// Server lifecycle
// ---------------------------------------------------------------------------

func TestStartNonBlockingAndStop(t *testing.T) {
	t.Parallel()
	s := prof.New(prof.WithPort(0))
	require.NoError(t, s.StartNonBlocking())
	time.Sleep(50 * time.Millisecond)
	s.Stop()
}

func TestStartBlockingAlreadyStarted(t *testing.T) {
	t.Parallel()
	s := prof.New(prof.WithPort(0))
	require.NoError(t, s.StartNonBlocking())
	defer s.Stop()
	time.Sleep(50 * time.Millisecond)

	err := s.StartBlocking()
	assert.Error(t, err)
}

func TestStopWithoutStart(t *testing.T) {
	t.Parallel()
	s := prof.New()
	s.Stop() // should not panic
}

// ---------------------------------------------------------------------------
// CPU Profiling
// ---------------------------------------------------------------------------

func TestStartAndStopCPUProfiling(t *testing.T) {
	t.Parallel()
	cpuProfMu.Lock()
	defer cpuProfMu.Unlock()

	dir := t.TempDir()
	s := prof.New(prof.WithOutputDir(dir))

	require.NoError(t, s.StartCPUProfiling(1))

	require.NoError(t, s.StopCPUProfiling())
}

func TestStartCPUProfilingAlreadyRunning(t *testing.T) {
	t.Parallel()
	cpuProfMu.Lock()
	defer cpuProfMu.Unlock()

	dir := t.TempDir()
	s := prof.New(prof.WithOutputDir(dir))

	require.NoError(t, s.StartCPUProfiling(10))
	defer s.StopCPUProfiling() // nolint: errcheck

	assert.Error(t, s.StartCPUProfiling(10))
}

func TestStopCPUProfilingIdempotent(t *testing.T) {
	t.Parallel()
	cpuProfMu.Lock()
	defer cpuProfMu.Unlock()

	s := prof.New()

	assert.NoError(t, s.StopCPUProfiling())
	assert.NoError(t, s.StopCPUProfiling())
}

// ---------------------------------------------------------------------------
// Heap profiling
// ---------------------------------------------------------------------------

func TestWriteHeap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := prof.New(prof.WithOutputDir(dir))

	path, err := s.WriteHeap()
	require.NoError(t, err)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Positive(t, fi.Size(), "heap profile should be non-empty")
	assert.Equal(t, ".pprof", filepath.Ext(path))
}

// ---------------------------------------------------------------------------
// Goroutine profiling
// ---------------------------------------------------------------------------

func TestWriteGoroutines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := prof.New(prof.WithOutputDir(dir))

	path, err := s.WriteGoroutines()
	require.NoError(t, err)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Positive(t, fi.Size(), "goroutine profile should be non-empty")
}

// ---------------------------------------------------------------------------
// HTTP Handlers
// ---------------------------------------------------------------------------

func setupTestRouter(s *prof.Server) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	s.AttachRoutes(r)
	return r
}

func TestStatusHandler(t *testing.T) {
	t.Parallel()
	s := prof.New()
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/status", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, false, body["profiling"])
}

func TestStartCPUHandler(t *testing.T) {
	t.Parallel()
	cpuProfMu.Lock()
	defer cpuProfMu.Unlock()

	dir := t.TempDir()
	s := prof.New(prof.WithOutputDir(dir))
	r := setupTestRouter(s)
	defer s.StopCPUProfiling() // nolint: errcheck

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/cpu/start?seconds=2", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "profiling_started", body["status"])
}

func TestStartCPUHandlerInvalidSeconds(t *testing.T) {
	t.Parallel()
	s := prof.New()
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/cpu/start?seconds=abc", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestStopCPUHandler(t *testing.T) {
	t.Parallel()
	cpuProfMu.Lock()
	defer cpuProfMu.Unlock()

	dir := t.TempDir()
	s := prof.New(prof.WithOutputDir(dir))
	r := setupTestRouter(s)

	require.NoError(t, s.StartCPUProfiling(10))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/cpu/stop", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDownloadHandlerNoProfile(t *testing.T) {
	t.Parallel()
	s := prof.New()
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/download", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHeapHandler(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := prof.New(prof.WithOutputDir(dir))
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/heap", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGoroutinesHandler(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := prof.New(prof.WithOutputDir(dir))
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/goroutines", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBlockHandlers(t *testing.T) {
	t.Parallel()
	s := prof.New()
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/block/start?rate=100", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/block/stop", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBlockStartHandlerInvalidRate(t *testing.T) {
	t.Parallel()
	s := prof.New()
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/block/start?rate=abc", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMutexHandlers(t *testing.T) {
	t.Parallel()
	s := prof.New()
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/mutex/start?fraction=5", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/mutex/stop", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMutexStartHandlerInvalidFraction(t *testing.T) {
	t.Parallel()
	s := prof.New()
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/mutex/start?fraction=abc", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCleanupHandler(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := prof.New(prof.WithOutputDir(dir))
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/cleanup?retention_hours=1", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "manual_cleanup_done", body["status"])
}

func TestCleanupHandlerInvalidRetention(t *testing.T) {
	t.Parallel()
	s := prof.New()
	r := setupTestRouter(s)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/cleanup?retention_hours=abc", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
