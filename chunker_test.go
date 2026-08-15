package fastcdc

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"math/rand"
	"testing"
)

func TestNewChunkerValidation(t *testing.T) {
	_, err := NewChunker(nil, DefaultOptions())
	if !errors.Is(err, ErrNilReader) {
		t.Fatalf("expected ErrNilReader, got %v", err)
	}

	invalidCases := []struct {
		name string
		opts Options
		want error
	}{
		{
			name: "min too small",
			opts: Options{MinSize: 32 * 1024, AvgSize: 256 * 1024, MaxSize: 512 * 1024},
			want: ErrInvalidMinSize,
		},
		{
			name: "avg not larger than min",
			opts: Options{MinSize: 64 * 1024, AvgSize: 64 * 1024, MaxSize: 512 * 1024},
			want: ErrInvalidAvgSize,
		},
		{
			name: "max not larger than avg",
			opts: Options{MinSize: 64 * 1024, AvgSize: 128 * 1024, MaxSize: 128 * 1024},
			want: ErrInvalidMaxSize,
		},
		{
			name: "max too large",
			opts: Options{MinSize: 64 * 1024, AvgSize: 128 * 1024, MaxSize: 300 * 1024 * 1024},
			want: ErrMaxSizeTooLarge,
		},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewChunker(bytes.NewReader(nil), tc.opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestChunkReconstructionAndInvariants(t *testing.T) {
	opts := Options{
		MinSize: 64 * 1024,
		AvgSize: 256 * 1024,
		MaxSize: 512 * 1024,
	}

	data := deterministicData(3*1024*1024+12345, 1337)
	chunker, err := NewChunker(bytes.NewReader(data), opts)
	if err != nil {
		t.Fatalf("new chunker: %v", err)
	}

	var reconstructed []byte
	var expectedOffset uint64

	for {
		ch, err := chunker.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}

		if ch.Offset != expectedOffset {
			t.Fatalf("offset mismatch: got %d want %d", ch.Offset, expectedOffset)
		}
		if ch.Length <= 0 {
			t.Fatalf("invalid chunk length: %d", ch.Length)
		}

		nextEnd := int(ch.Offset) + ch.Length
		if nextEnd < len(data) {
			if ch.Length < opts.MinSize || ch.Length > opts.MaxSize {
				t.Fatalf("non-tail chunk length out of range: %d", ch.Length)
			}
		} else if ch.Length > opts.MaxSize {
			t.Fatalf("tail chunk length too large: %d", ch.Length)
		}

		expectedOffset += uint64(ch.Length)
		reconstructed = append(reconstructed, ch.Data...)
	}

	if expectedOffset != uint64(len(data)) {
		t.Fatalf("total bytes mismatch: got %d want %d", expectedOffset, len(data))
	}
	if !bytes.Equal(reconstructed, data) {
		t.Fatalf("reconstructed content differs from input")
	}
}

func TestEdgeSizedInputs(t *testing.T) {
	opts := Options{
		MinSize: 64 * 1024,
		AvgSize: 256 * 1024,
		MaxSize: 512 * 1024,
	}

	sizes := []int{
		0,
		1,
		opts.MinSize - 1,
		opts.MinSize,
		opts.MinSize + 1,
		opts.MaxSize,
		opts.MaxSize + 17,
	}

	for _, size := range sizes {
		t.Run("size_"+itoa(size), func(t *testing.T) {
			data := deterministicData(size, int64(size+7))
			chunks := collectChunks(t, bytes.NewReader(data), opts)

			var out []byte
			for _, ch := range chunks {
				out = append(out, ch.Data...)
			}
			if !bytes.Equal(out, data) {
				t.Fatalf("roundtrip mismatch for size %d", size)
			}

			if size == 0 && len(chunks) != 0 {
				t.Fatalf("expected no chunks for empty input, got %d", len(chunks))
			}
			if size > 0 && size <= opts.MinSize && len(chunks) != 1 {
				t.Fatalf("expected exactly one chunk for size %d, got %d", size, len(chunks))
			}
		})
	}
}

func TestPassingConditions_DeduplicationCheck(t *testing.T) {
	opts := Options{
		MinSize: 64 * 1024,
		AvgSize: 256 * 1024,
		MaxSize: 512 * 1024,
	}

	pattern := deterministicData(1024*1024, 20240601)
	file := make([]byte, 0, 10*1024*1024)
	for i := 0; i < 10; i++ {
		file = append(file, pattern...)
	}

	chunks := collectChunks(t, bytes.NewReader(file), opts)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk for repeated-pattern input")
	}

	seen := make(map[[32]byte]int)
	for _, ch := range chunks {
		h := sha256.Sum256(ch.Data)
		seen[h]++
	}

	repeated := 0
	for _, count := range seen {
		if count > 1 {
			repeated += count - 1
		}
	}
	if repeated == 0 {
		t.Fatal("expected duplicate chunk content in repeated-pattern file")
	}
	if len(seen) >= len(chunks) {
		t.Fatalf("expected repeated chunk patterns to reduce unique chunk count: unique=%d total=%d", len(seen), len(chunks))
	}
}

func TestPassingConditions_ByteShiftResistanceCheck(t *testing.T) {
	opts := Options{
		MinSize: 64 * 1024,
		AvgSize: 256 * 1024,
		MaxSize: 512 * 1024,
	}

	fileV1 := deterministicData(8*1024*1024, 20250815)
	prefix := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	fileV2 := append(prefix, fileV1...)

	hashesV1 := chunkHashes(t, fileV1, opts)
	hashesV2 := chunkHashes(t, fileV2, opts)

	matched := 0
	counts := make(map[[32]byte]int, len(hashesV1))
	for _, h := range hashesV1 {
		counts[h]++
	}
	for _, h := range hashesV2 {
		if counts[h] > 0 {
			matched++
			counts[h]--
		}
	}

	matchRatio := float64(matched) / float64(len(hashesV1))
	if matchRatio < 0.80 {
		t.Fatalf("expected >=80%% hash match ratio, got %.2f%%", matchRatio*100)
	}
}

func TestByteShiftResistanceByContentHash(t *testing.T) {
	opts := Options{
		MinSize: 64 * 1024,
		AvgSize: 256 * 1024,
		MaxSize: 512 * 1024,
	}

	fileV1 := deterministicData(8*1024*1024, 20250815)
	prefix := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE}
	fileV2 := append(prefix, fileV1...)

	hashesV1 := chunkHashes(t, fileV1, opts)
	hashesV2 := chunkHashes(t, fileV2, opts)

	matched := 0
	counts := make(map[[32]byte]int, len(hashesV1))
	for _, h := range hashesV1 {
		counts[h]++
	}
	for _, h := range hashesV2 {
		if counts[h] > 0 {
			matched++
			counts[h]--
		}
	}

	matchRatio := float64(matched) / float64(len(hashesV1))
	if matchRatio < 0.80 {
		t.Fatalf("expected >=80%% hash match ratio, got %.2f%%", matchRatio*100)
	}
}

func TestReaderBehaviors(t *testing.T) {
	opts := Options{
		MinSize: 64 * 1024,
		AvgSize: 256 * 1024,
		MaxSize: 512 * 1024,
	}

	t.Run("short reads reconstruct", func(t *testing.T) {
		data := deterministicData(2*1024*1024+77, 777)
		r := &fixedStepReader{data: data, step: 997}
		chunks := collectChunks(t, r, opts)

		var out []byte
		for _, ch := range chunks {
			out = append(out, ch.Data...)
		}
		if !bytes.Equal(out, data) {
			t.Fatalf("short-read reconstruction mismatch")
		}
	})

	t.Run("zero progress returns err no progress", func(t *testing.T) {
		chunker, err := NewChunker(zeroProgressReader{}, opts)
		if err != nil {
			t.Fatalf("new chunker: %v", err)
		}

		_, err = chunker.Next()
		if !errors.Is(err, io.ErrNoProgress) {
			t.Fatalf("expected io.ErrNoProgress, got %v", err)
		}
	})

	t.Run("data plus error is drained before surfacing error", func(t *testing.T) {
		errBoom := errors.New("boom")
		payload := deterministicData(1024, 11)
		chunker, err := NewChunker(&dataThenErrorReader{data: payload, err: errBoom}, opts)
		if err != nil {
			t.Fatalf("new chunker: %v", err)
		}

		ch, err := chunker.Next()
		if err != nil {
			t.Fatalf("unexpected first error: %v", err)
		}
		if !bytes.Equal(ch.Data, payload) {
			t.Fatalf("did not receive buffered payload before error")
		}

		_, err = chunker.Next()
		if !errors.Is(err, errBoom) {
			t.Fatalf("expected %v, got %v", errBoom, err)
		}
	})
}

func TestNextViewMatchesNextBoundaries(t *testing.T) {
	opts := Options{
		MinSize: 64 * 1024,
		AvgSize: 256 * 1024,
		MaxSize: 512 * 1024,
	}
	data := deterministicData(3*1024*1024+1, 4242)

	chunks := collectChunks(t, bytes.NewReader(data), opts)

	chunker, err := NewChunker(bytes.NewReader(data), opts)
	if err != nil {
		t.Fatalf("new chunker: %v", err)
	}

	var views []ChunkView
	for {
		v, err := chunker.NextView()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("next view: %v", err)
		}
		views = append(views, v)
	}

	if len(chunks) != len(views) {
		t.Fatalf("chunk count mismatch: next=%d nextView=%d", len(chunks), len(views))
	}
	for i := range chunks {
		if chunks[i].Offset != views[i].Offset || chunks[i].Length != views[i].Length {
			t.Fatalf("boundary mismatch at %d", i)
		}
	}
}

func collectChunks(t *testing.T, r io.Reader, opts Options) []Chunk {
	t.Helper()
	chunker, err := NewChunker(r, opts)
	if err != nil {
		t.Fatalf("new chunker: %v", err)
	}

	var chunks []Chunk
	for {
		ch, err := chunker.Next()
		if err == io.EOF {
			return chunks
		}
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		chunks = append(chunks, ch)
	}
}

func chunkHashes(t *testing.T, data []byte, opts Options) [][32]byte {
	t.Helper()
	chunks := collectChunks(t, bytes.NewReader(data), opts)
	out := make([][32]byte, 0, len(chunks))
	for _, ch := range chunks {
		out = append(out, sha256.Sum256(ch.Data))
	}
	return out
}

func deterministicData(size int, seed int64) []byte {
	data := make([]byte, size)
	r := rand.New(rand.NewSource(seed))
	for i := range data {
		data[i] = byte(r.Intn(256))
	}
	return data
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

type fixedStepReader struct {
	data []byte
	pos  int
	step int
}

func (r *fixedStepReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := r.step
	if n > len(p) {
		n = len(p)
	}
	remaining := len(r.data) - r.pos
	if n > remaining {
		n = remaining
	}
	copy(p, r.data[r.pos:r.pos+n])
	r.pos += n
	return n, nil
}

type zeroProgressReader struct{}

func (zeroProgressReader) Read(_ []byte) (int, error) {
	return 0, nil
}

type dataThenErrorReader struct {
	data []byte
	err  error
	read bool
}

func (r *dataThenErrorReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, r.err
	}
	r.read = true
	n := copy(p, r.data)
	return n, r.err
}
