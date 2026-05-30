# Advanced Runtime Profiling Toolkit for Go (Gin + pprof)

[![Go Version](https://img.shields.io/badge/Go-1.23%2B-blue)](https://go.dev/)
[![Test Status](https://github.com/alex-cos/prof/actions/workflows/test.yml/badge.svg)](https://github.com/alex-cos/prof/actions/workflows/test.yml)
[![Codecov](https://codecov.io/gh/alex-cos/prof/branch/main/graph/badge.svg)](https://codecov.io/gh/alex-cos/prof)
[![Lint Status](https://github.com/alex-cos/prof/actions/workflows/lint.yml/badge.svg)](https://github.com/alex-cos/prof/actions/workflows/lint.yml)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/alex-cos/prof)](https://goreportcard.com/report/github.com/alex-cos/prof)

`prof` is a production‑grade profiling module for Go services. It exposes runtime profiling features through both:

![prof](/prof.png)

- A standalone internal HTTP pprof server
- Gin HTTP routes for dynamic start/stop profiling and on‑demand snapshots

It provides controlled CPU profiling, heap dumps, goroutine snapshots, block/mutex profiling, download endpoints, and automated or manual cleanup of profiling files.

## Features

- **Standard pprof handlers** mounted under `/debug/pprof/*`
- **Start/stop CPU profiling on demand** (`/cpu/start`, `/cpu/stop`)
- **Heap profile dump** (`/heap`)
- **Goroutine dump** (`/goroutines`)
- **Block profile control** (`/block/start`, `/block/stop`)
- **Mutex contention profile control** (`/mutex/start`, `/mutex/stop`)
- **File retention & cleanup**
  - Auto-clean daily old profiling files
  - Optional cleanup after download
  - Manual cleanup endpoint (`POST /cleanup`) with configurable retention
- **Custom output directory** for generated profiles
- **Structured logging support** via slog-compatible interface

---

## Installation

```bash
go get github.com/alex-cos/prof
```

---

## Quick Start

### 1. Create the profiling server

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
pp := prof.New(
  prof.WithHost("127.0.0.1"),
  prof.WithPort(6060),
  prof.WithSlog(logger),
  prof.WithOutputDir("./profiles"),
)
pp.StartNonBlocking() // runs the internal pprof server
defer pp.Stop()
```

### 2. Attach profiling routes to your Gin server

```go
r := gin.Default()
pp.AttachRoutes(r)
r.Run(":8080")
```

---

## Usage Examples

### Start CPU profiling

```bash
POST /cpu/start?seconds=30
```

### Stop CPU profiling manually

```bash
POST /cpu/stop
```

### Download last completed CPU profile

```bash
GET /download
```

### Capture a heap profile

```bash
POST /heap
```

### Capture goroutine dump

```bash
GET /goroutines
```

### Enable block profiling

```bash
POST /block/start?rate=1
```

### Disable block profiling

```bash
POST /block/stop
```

### Manual cleanup (delete profiles older than 24h)

```bash
POST /cleanup
```

Or specify custom retention:

```bash
POST /cleanup?retention_hours=2
```

---

## File Retention

`prof` supports two retention mechanisms:

### 1. Delete file after download

Automatically removes CPU profiles after they are downloaded.

### 2. Daily cleanup

Call once during initialization:

```go
pp.RunDailyCleanup(24 * time.Hour)
```

---

## File Layout

Profile files are created using timestamped names:

```txt
cpu_YYYYMMDD_HHMMSS.pprof
heap_YYYYMMDD_HHMMSS.pprof
goroutine_YYYYMMDD_HHMMSS.txt
```
