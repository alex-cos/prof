package prof

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	rtpprof "runtime/pprof"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	DefaultPort            = 8060
	DefaultCPUSeconds      = 30
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultWriteTimeout    = time.Minute
	DefaultIdleTimeout     = time.Minute
	ShutdownTimeout        = 5 * time.Second
)

type Server struct {
	host     string
	port     int
	srv      *http.Server
	slog     Logger
	isActive bool
	mu       sync.Mutex
	// CPU profiling state
	profiling      bool
	cpuProfileFile *os.File
	profilePath    string
	// output directory for profiles
	outputDir string
	// block & mutex profiling state
	blockRate     int
	mutexFraction int
}

// -----------------------------------------------------------------------------
// Options
// -----------------------------------------------------------------------------

type Option func(*Server)

func WithHost(host string) Option {
	return func(c *Server) {
		c.host = host
	}
}

func WithPort(port int) Option {
	return func(c *Server) {
		c.port = port
	}
}

func WithSlog(slog Logger) Option {
	return func(c *Server) {
		c.slog = slog
	}
}

func WithOutputDir(outputDir string) Option {
	return func(c *Server) {
		c.outputDir = outputDir
	}
}

// -----------------------------------------------------------------------------
// Constructor
// -----------------------------------------------------------------------------

func New(opts ...Option) *Server {
	outputDir := os.TempDir()

	s := &Server{
		host:           "",
		port:           DefaultPort,
		srv:            nil,
		slog:           nil,
		isActive:       false,
		mu:             sync.Mutex{},
		profiling:      false,
		cpuProfileFile: nil,
		profilePath:    "",
		outputDir:      outputDir,
		blockRate:      0,
		mutexFraction:  0,
	}

	for _, o := range opts {
		o(s)
	}

	return s
}

// ---------------------------------------------------------------------------
// Start / Stop server
// ---------------------------------------------------------------------------

func (s *Server) addr() string {
	return fmt.Sprintf("%s:%d", s.host, s.port)
}

func (s *Server) init() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := &http.Server{
		Addr:              s.addr(),
		Handler:           mux,
		TLSConfig:         nil,
		ReadTimeout:       0,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		WriteTimeout:      DefaultWriteTimeout,
		IdleTimeout:       DefaultIdleTimeout,
		MaxHeaderBytes:    0,
	}

	return srv
}

func (s *Server) StartBlocking() error {
	s.mu.Lock()
	if s.srv != nil {
		s.mu.Unlock()
		s.logWarn("prof server already started", slog.String("addr", s.addr()))
		return fmt.Errorf("prof server already started on %s", s.addr())
	}
	s.logInfo("starting prof server", slog.String("addr", s.addr()))
	s.srv = s.init()
	s.mu.Unlock()
	err := s.srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logError("prof ListenAndServe error", slog.String("addr", s.addr()), slog.Any("error", err))
		s.mu.Lock()
		s.srv = nil
		s.mu.Unlock()
		return err
	}
	s.logInfo("prof server stopped", slog.String("addr", s.addr()))
	s.mu.Lock()
	s.srv = nil
	s.mu.Unlock()

	return nil
}

func (s *Server) StartNonBlocking() error {
	go func() {
		_ = s.StartBlocking()
	}()

	return nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()

	if srv == nil {
		return
	}
	s.logInfo("prof server shutdown requested", slog.String("addr", s.addr()))
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		s.logError("error shutting down prof server", slog.String("addr", s.addr()), slog.Any("error", err))
	}
}

// ---------------------------------------------------------------------------
// Profiling control (start/stop)
// ---------------------------------------------------------------------------

// StartCPUProfiling starts a CPU profile for `seconds` seconds. If seconds<=0 uses 30s.
func (s *Server) StartCPUProfiling(seconds int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.profiling {
		return errors.New("profiling already running")
	}

	if seconds <= 0 {
		seconds = DefaultCPUSeconds
	}

	ts := time.Now().Format("20060102_150405")
	path := filepath.Join(s.outputDir, fmt.Sprintf("cpu_%s.pprof", ts))
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	if err := rtpprof.StartCPUProfile(f); err != nil {
		f.Close()
		return err
	}

	s.cpuProfileFile = f
	s.profilePath = path
	s.profiling = true

	// Auto-stop with logging of result
	go func(d int, p string) {
		time.Sleep(time.Duration(d) * time.Second)
		if err := s.StopCPUProfiling(); err != nil {
			s.logError("auto-stop profiling failed", slog.Any("error", err))
		} else {
			s.logInfo("profiling auto-stopped", slog.String("file", p))
		}
	}(seconds, path)

	return nil
}

// StopCPUProfiling stops CPU profiling. It is safe to call multiple times.
func (s *Server) StopCPUProfiling() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.profiling {
		// idempotent
		return nil
	}

	rtpprof.StopCPUProfile()
	if s.cpuProfileFile != nil {
		_ = s.cpuProfileFile.Close()
	}

	s.profiling = false
	s.logInfo("CPU profiling stopped", slog.String("file", s.profilePath))
	return nil
}

// ---------------------------------------------------------------------------
// Heap snapshot
// ---------------------------------------------------------------------------

// WriteHeap writes a heap profile to a file and returns the path.
func (s *Server) WriteHeap() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ts := time.Now().Format("20060102_150405")
	path := filepath.Join(s.outputDir, fmt.Sprintf("heap_%s.pprof", ts))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := rtpprof.WriteHeapProfile(f); err != nil {
		return "", err
	}

	s.logInfo("heap profile written", slog.String("file", path))
	return path, nil
}

// ---------------------------------------------------------------------------
// Goroutine snapshot
// ---------------------------------------------------------------------------

// WriteGoroutines writes the goroutine profile (stack traces) to a file, returns path.
func (s *Server) WriteGoroutines() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ts := time.Now().Format("20060102_150405")
	path := filepath.Join(s.outputDir, fmt.Sprintf("goroutine_%s.txt", ts))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	p := rtpprof.Lookup("goroutine")
	if p == nil {
		return "", errors.New("goroutine profile not available")
	}
	if err := p.WriteTo(f, 1); err != nil {
		return "", err
	}

	s.logInfo("goroutine profile written", slog.String("file", path))
	return path, nil
}

// ---------------------------------------------------------------------------
// Block & Mutex profiling
// ---------------------------------------------------------------------------

// StartBlockProfiling enables block profiling with rate (events/sample). rate==0 disables.
func (s *Server) StartBlockProfiling(rate int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	runtime.SetBlockProfileRate(rate)
	s.blockRate = rate
	s.logInfo("block profile rate set", slog.Int("rate", rate))
	return nil
}

// StopBlockProfiling disables block profiling.
func (s *Server) StopBlockProfiling() error {
	return s.StartBlockProfiling(0)
}

// StartMutexProfiling sets mutex profile fraction. fraction==0 disables.
func (s *Server) StartMutexProfiling(fraction int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	runtime.SetMutexProfileFraction(fraction)
	s.mutexFraction = fraction
	s.logInfo("mutex profile fraction set", slog.Int("fraction", fraction))
	return nil
}

// StopMutexProfiling disables mutex profiling.
func (s *Server) StopMutexProfiling() error {
	return s.StartMutexProfiling(0)
}

// ---------------------------------------------------------------------------
// File Retention & Cleanup
// ---------------------------------------------------------------------------

// RunDailyCleanup launches a goroutine that removes files older than retention duration.
func (s *Server) RunDailyCleanup(retention time.Duration) {
	go func() {
		for {
			time.Sleep(24 * time.Hour)
			s.cleanupOldFiles(retention)
		}
	}()
}

// ---------------------------------------------------------------------------
// Gin route attachment
// ---------------------------------------------------------------------------

// AttachRoutes attaches a set of endpoints to control profiling and to download snapshots.
func (s *Server) AttachRoutes(r gin.IRoutes) {
	r.POST("/cpu/start", s.startCPUHandler)
	r.POST("/cpu/stop", s.stopCPUHandler)
	r.GET("/status", s.statusHandler)
	r.GET("/download", s.downloadHandler)
	r.POST("/heap", s.heapHandler)
	r.GET("/goroutines", s.goroutinesHandler)
	r.POST("/block/start", s.startBlockHandler)
	r.POST("/block/stop", s.stopBlockHandler)
	r.POST("/mutex/start", s.startMutexHandler)
	r.POST("/mutex/stop", s.stopMutexHandler)
	r.POST("/cleanup", s.cleanupHandler)
}

// ----------------------------------------------------------------------------
// Unexported functions
// ----------------------------------------------------------------------------

// cleanupOldFiles removes all files in outputDir older than retention.
func (s *Server) cleanupOldFiles(retention time.Duration) {
	entries, err := os.ReadDir(s.outputDir)
	if err != nil {
		s.logError("cleanup failed: cannot read outputDir", slog.Any("error", err))
		return
	}
	cutoff := time.Now().Add(-retention)

	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			s.logError("cleanup file info error", slog.Any("error", err))
			continue
		}

		path := filepath.Join(s.outputDir, e.Name())
		if fi.ModTime().Before(cutoff) {
			s.logInfo("cleanup deleting stale profile", slog.String("file", path))
			_ = os.Remove(path)
		}
	}
}

func (s *Server) logInfo(msg string, args ...any) {
	if s.slog != nil {
		s.slog.Info(msg, args...)
	}
}

func (s *Server) logError(msg string, args ...any) {
	if s.slog != nil {
		s.slog.Error(msg, args...)
	}
}

func (s *Server) logWarn(msg string, args ...any) {
	if s.slog != nil {
		s.slog.Warn(msg, args...)
	}
}
