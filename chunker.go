package fastcdc

import (
	"io"
)

// Chunk represents a single sliced byte region.
type Chunk struct {
	Offset uint64
	Length int
	Data   []byte
}

// ChunkView references chunk bytes backed by Chunker internal storage.
// Data is only valid until the next call to Next/NextView.
type ChunkView struct {
	Offset uint64
	Length int
	Data   []byte
}

// Chunker wraps an io.Reader and slices data using FastCDC.
type Chunker struct {
	r          io.Reader
	cfg        internalConfig
	buf        []byte
	head       int
	tail       int
	offset     uint64
	eofReached bool
	pendingErr error
}

// NewChunker creates a ready-to-use FastCDC Chunker.
func NewChunker(r io.Reader, opts Options) (*Chunker, error) {
	if r == nil {
		return nil, ErrNilReader
	}

	cfg, err := opts.validateAndBuild()
	if err != nil {
		return nil, err
	}

	// Buffer sized to 2x MaxSize to ensure continuous streaming capacity.
	// MaxSize is validated in config to keep this allocation bounded.
	bufSize := cfg.MaxSize * 2

	return &Chunker{
		r:   r,
		cfg: cfg,
		buf: make([]byte, bufSize),
	}, nil
}

// Next retrieves the next chunk from the stream. Returns io.EOF when done.
func (c *Chunker) Next() (Chunk, error) {
	view, err := c.nextView()
	if err != nil {
		return Chunk{}, err
	}

	chunkData := make([]byte, view.Length)
	copy(chunkData, view.Data)

	return Chunk{
		Offset: view.Offset,
		Length: view.Length,
		Data:   chunkData,
	}, nil
}

// NextView retrieves the next chunk without copying data.
// Returned bytes are invalidated by the next call to Next/NextView.
func (c *Chunker) NextView() (ChunkView, error) {
	return c.nextView()
}

func (c *Chunker) nextView() (ChunkView, error) {
	if err := c.fillBuffer(); err != nil && err != io.EOF {
		return ChunkView{}, err
	}

	available := c.tail - c.head
	if available == 0 {
		return ChunkView{}, io.EOF
	}

	// Determine max scan bounds for current chunk
	maxScan := c.cfg.MaxSize
	if available < maxScan {
		maxScan = available
	}

	// If available data is smaller than MinSize, return remaining byte slice.
	if available <= c.cfg.MinSize {
		return c.emitChunkView(available), nil
	}

	// Fast CDC Gear Scan Loop
	curr := c.head + c.cfg.MinSize - 1
	limit := c.head + maxScan - 1
	target := c.head + c.cfg.AvgSize - 1

	var fp uint64

	// Sub-target region: Stricter mask
	for ; curr <= target && curr <= limit; curr++ {
		fp = (fp << 1) + gearTable[c.buf[curr]]
		if (fp & c.cfg.maskStrict) == 0 {
			return c.emitChunkView(curr - c.head + 1), nil
		}
	}

	// Post-target region: Relaxed mask
	for ; curr <= limit; curr++ {
		fp = (fp << 1) + gearTable[c.buf[curr]]
		if (fp & c.cfg.maskRelax) == 0 {
			return c.emitChunkView(curr - c.head + 1), nil
		}
	}

	// Reached MaxSize limit without finding a natural cut point
	return c.emitChunkView(maxScan), nil
}

func (c *Chunker) fillBuffer() error {
	if c.eofReached {
		return io.EOF
	}

	// Slide unconsumed bytes to the start of the buffer if needed
	if c.head > 0 {
		copy(c.buf, c.buf[c.head:c.tail])
		c.tail -= c.head
		c.head = 0
	}

	if c.pendingErr != nil {
		if c.head == c.tail {
			err := c.pendingErr
			c.pendingErr = nil
			if err == io.EOF {
				c.eofReached = true
			}
			return err
		}
		return nil
	}

	// Read as much data as possible into the remaining buffer space
	stalledReads := 0
	for c.tail < len(c.buf) {
		n, err := c.r.Read(c.buf[c.tail:])
		c.tail += n

		if n == 0 && err == nil {
			stalledReads++
			if stalledReads >= 4 {
				return io.ErrNoProgress
			}
			continue
		}
		stalledReads = 0

		if err != nil {
			if n > 0 {
				c.pendingErr = err
				return nil
			}
			if err == io.EOF {
				c.eofReached = true
			}
			return err
		}
	}

	return nil
}

func (c *Chunker) emitChunkView(length int) ChunkView {
	chunk := ChunkView{
		Offset: c.offset,
		Length: length,
		Data:   c.buf[c.head : c.head+length],
	}

	c.head += length
	c.offset += uint64(length)

	return chunk
}
