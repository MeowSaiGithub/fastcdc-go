# fastcdc-go

FastCDC content-defined chunking implementation in Go.

## API

- `NewChunker(reader, options)` creates a chunker.
- `(*Chunker).Next()` returns copied chunk data (`Chunk`).
- `(*Chunker).NextView()` returns zero-copy chunk data (`ChunkView`) backed by internal buffer; data is valid only until the next `Next` or `NextView` call.

## Options

Default configuration:

```go
fastcdc.DefaultOptions()
// MinSize: 1MB, AvgSize: 4MB, MaxSize: 8MB
```

Custom example:

```go
fastcdc.Options{
    MinSize: 64 * 1024,
    AvgSize: 256 * 1024,
    MaxSize: 512 * 1024,
}
```

Constraints:
- `MinSize >= 64KB`
- `AvgSize > MinSize`
- `MaxSize > AvgSize`

## Test and benchmark

```bash
go test ./...
go test -run ^$ -bench . -benchmem
```

## Benchmark result (Windows)

Environment:
- OS: Windows
- CPU: 12th Gen Intel(R) Core(TM) i7-12650H
- Command: `go test -run ^$ -bench . -benchmem -benchtime=1x`
- Dataset: 64MB for each benchmark case
- Chunk options used in benchmark: `DefaultOptions()` (`1MB/4MB/8MB`)

| Benchmark | ns/op | MB/s | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| `BenchmarkChunker/random/copy-16` | 88,897,700 | 754.90 | 83,935,408 | 15 |
| `BenchmarkChunker/random/view-16` | 63,121,600 | 1,063.17 | 16,777,392 | 3 |
| `BenchmarkChunker/repetitive/copy-16` | 76,598,700 | 876.11 | 83,886,272 | 12 |
| `BenchmarkChunker/repetitive/view-16` | 59,029,600 | 1,136.87 | 16,777,392 | 3 |

Quick interpretation:
- `view` is faster than `copy` and uses far fewer allocations because it avoids per-chunk byte copying.
- repetitive data is slightly faster than random data in this run.
- numbers are machine/runtime dependent; rerun on your target environment for release-level baselines.

Safety notes:
- `MaxSize` is capped at `256MB` to keep internal buffer allocation bounded (`2 * MaxSize`).
- mask construction is guarded to avoid out-of-range bit shifts on extreme size values.