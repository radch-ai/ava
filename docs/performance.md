# Performance and Scaling

## Benchmarks

Run benchmarks with:

```bash
go test -bench=. -benchmem ./internal/websocket/
```

## Benchmark Results

### Message Throughput

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkWebSocketSmallMessages` | 4-byte messages |
| `BenchmarkWebSocketMediumMessages` | 1KB messages |
| `BenchmarkWebSocketLargeMessages` | 8KB messages (buffer size) |
| `BenchmarkActiveConns` | Connection counter reads |

## Memory Optimization

### sync.Pool Usage

Message structs are allocated from a pool to reduce garbage collection pressure:

```go
var wsMsgPool = sync.Pool{
    New: func() interface{} {
        return &wsMessage{}
    },
}
```

**Impact:** Reduces allocations for high-frequency message operations.

### Buffer Sizes

| Buffer | Size | Rationale |
|--------|------|-----------|
| Read/Write | 8KB | Balance between memory and throughput |
| Max Message | 64KB | Prevent memory exhaustion attacks |
| writeCh | 8 messages | Buffered channel for writes |

## Connection Memory

Per-connection memory:

```
Read Buffer:  8 KB
Write Buffer: 8 KB
Total:        16 KB per connection
```

### Memory at Scale

| Connections | Memory (buffers) |
|-------------|------------------|
| 1,000 | 16 MB |
| 10,000 | 160 MB |
| 100,000 | 1.6 GB |

## Vertical Scaling

### Single Instance Limits

Based on buffer memory alone, a single instance can theoretically handle:

- **8 GB RAM:** ~500K connections
- **16 GB RAM:** ~1M connections

**Practical limits are lower** due to:
- Goroutine overhead
- HTTP server overhead
- Logging buffers
- OS socket limits

### File Descriptor Limits

Check and adjust OS limits:

```bash
# Check current limit
ulimit -n

# Increase temporarily
ulimit -n 65536

# Persist in /etc/security/limits.conf:
# * soft nofile 65536
# * hard nofile 65536
```

## Horizontal Scaling

For production deployment, use multiple gateway instances behind a load balancer:

```
                Load Balancer
                     |
      +--------------+--------------+
      |              |              |
   Gateway 1     Gateway 2      Gateway 3
```

### Load Balancer Configuration

- **Algorithm:** Least connections
- **Health Check:** `/health` endpoint
- **WebSocket:** Enable proxying (e.g., `proxy_set_header Upgrade $http_upgrade`)

### Connection Limiting

Use `GATEWAY_MAX_CONNS` to prevent any single instance from being overwhelmed:

```bash
GATEWAY_MAX_CONNS=10000
```

Connections exceeding the limit receive `503 Service Unavailable`.

## Performance Tuning

### Write Channel Buffer

The write channel buffers 8 messages by default:

```go
writeCh := make(chan *wsMessage, 8)
```

Increase for high-throughput scenarios:

```go
writeCh := make(chan *wsMessage, 32)
```

### Read Timeout

Default 60-second read timeout closes idle connections:

```go
const readTimeout = 60 * time.Second
```

Decrease for faster cleanup, increase for slower clients.

### Ping Interval

Default 30-second ping interval:

```go
const pingInterval = 30 * time.Second
```

Decrease for faster dead connection detection (increases bandwidth).

## Profiling

Enable CPU profiling:

```go
import _ "net/http/pprof"

// Add pprof endpoint to mux
mux.HandleFunc("/debug/pprof/", pprof.Index)
```

Run with profiling:

```bash
go tool pprof http://localhost:8080/debug/pprof/profile?seconds=30
```

## Monitoring

### Active Connections

The handler exposes an `ActiveConns()` method for monitoring:

```go
count := handler.ActiveConns()
```

### Structured Logging

All events include context fields:

```json
{
  "level": "info",
  "conn_id": "123e4567-e89b-12d3-a456-426614174000",
  "remote_addr": "192.168.1.100:12345",
  "active_conns": 1234,
  "message": "websocket connection established"
}
```

## Production Checklist

- [ ] Set `GATEWAY_ALLOWED_ORIGINS` to specific origins
- [ ] Set `GATEWAY_MAX_CONNS` based on available memory
- [ ] Configure OS file descriptor limits
- [ ] Set up load balancer with WebSocket support
- [ ] Enable health checks on `/health`
- [ ] Configure logging aggregation
- [ ] Set up metrics collection (active connections, message rates)
- [ ] Profile under load
- [ ] Test graceful shutdown with active connections
