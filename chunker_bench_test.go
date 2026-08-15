package fastcdc

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
)

func BenchmarkChunker(b *testing.B) {
	opts := DefaultOptions()
	const dataSize = 64 * 1024 * 1024

	randomData := benchmarkData(dataSize, 1001)
	repetitiveData := benchmarkPatternData(dataSize)

	benchmarks := []struct {
		name    string
		data    []byte
		useView bool
	}{
		{name: "random/copy", data: randomData, useView: false},
		{name: "random/view", data: randomData, useView: true},
		{name: "repetitive/copy", data: repetitiveData, useView: false},
		{name: "repetitive/view", data: repetitiveData, useView: true},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.SetBytes(int64(len(bm.data)))
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				chunker, err := NewChunker(bytes.NewReader(bm.data), opts)
				if err != nil {
					b.Fatalf("new chunker: %v", err)
				}

				if bm.useView {
					for {
						_, err := chunker.NextView()
						if err == io.EOF {
							break
						}
						if err != nil {
							b.Fatalf("next view: %v", err)
						}
					}
					continue
				}

				for {
					_, err := chunker.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Fatalf("next: %v", err)
					}
				}
			}
		})
	}
}

func benchmarkData(size int, seed int64) []byte {
	out := make([]byte, size)
	r := rand.New(rand.NewSource(seed))
	for i := range out {
		out[i] = byte(r.Intn(256))
	}
	return out
}

func benchmarkPatternData(size int) []byte {
	base := []byte("FASTCDC_PATTERN_BLOCK_0123456789abcdef")
	out := make([]byte, size)
	for i := 0; i < size; i += len(base) {
		copy(out[i:], base)
	}
	return out
}
