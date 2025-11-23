package prof

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// start CPU profile (auto-stop after seconds).
func (s *Server) startCPUHandler(c *gin.Context) {
	sec := 30
	if v := c.Query("seconds"); v != "" {
		fmt.Sscanf(v, "%d", &sec)
	}

	if err := s.StartCPUProfiling(sec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "profiling_started", "duration": sec})
}

// stop CPU profile.
func (s *Server) stopCPUHandler(c *gin.Context) {
	if err := s.StopCPUProfiling(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "profiling_stopped"})
}

// returns profiling state and last cpu file path.
func (s *Server) statusHandler(c *gin.Context) {
	s.mu.Lock()
	active := s.profiling
	path := s.profilePath
	s.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{"profiling": active, "file": path})
}

// download last completed CPU profile.
func (s *Server) downloadHandler(c *gin.Context) {
	s.mu.Lock()
	p := s.profilePath
	busy := s.profiling
	s.mu.Unlock()

	if busy || p == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no completed cpu profile available yet"})
		return
	}
	c.File(p)
	// attempt deletion after download
	go func() { _ = os.Remove(p) }()
}

// write heap profile and download it.
func (s *Server) heapHandler(c *gin.Context) {
	path, err := s.WriteHeap()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.File(path)
}

// write goroutine dump and download it.
func (s *Server) goroutinesHandler(c *gin.Context) {
	path, err := s.WriteGoroutines()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.File(path)
}

// enable block profiling.
func (s *Server) startBlockHandler(c *gin.Context) {
	rate := 1
	if v := c.Query("rate"); v != "" {
		fmt.Sscanf(v, "%d", &rate)
	}
	if err := s.StartBlockProfiling(rate); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "block_profiling_set", "rate": rate})
}

// disable block profiling.
func (s *Server) stopBlockHandler(c *gin.Context) {
	if err := s.StopBlockProfiling(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "block_profiling_disabled"})
}

// enable mutex profiling (fraction).
func (s *Server) startMutexHandler(c *gin.Context) {
	fraction := 0
	if v := c.Query("fraction"); v != "" {
		fmt.Sscanf(v, "%d", &fraction)
	}
	if err := s.StartMutexProfiling(fraction); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "mutex_profiling_set", "fraction": fraction})
}

// disable mutex profiling.
func (s *Server) stopMutexHandler(c *gin.Context) {
	if err := s.StopMutexProfiling(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "mutex_profiling_disabled"})
}

// cleanup old files.
func (s *Server) cleanupHandler(c *gin.Context) {
	rent := 24 * time.Hour
	if v := c.Query("retention_hours"); v != "" {
		var h int
		fmt.Sscanf(v, "%d", &h)
		if h > 0 {
			rent = time.Duration(h) * time.Hour
		}
	}

	s.cleanupOldFiles(rent)
	c.JSON(200, gin.H{"status": "manual_cleanup_done", "retention": rent.String()})
}
