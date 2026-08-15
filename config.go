package fastcdc

import (
	"errors"
	"math/bits"
)

var (
	ErrNilReader         = errors.New("reader must not be nil")
	ErrInvalidMinSize    = errors.New("MinSize must be >= 64KB")
	ErrInvalidAvgSize    = errors.New("AvgSize must be > MinSize")
	ErrInvalidMaxSize    = errors.New("MaxSize must be > AvgSize")
	ErrMaxSizeTooLarge   = errors.New("MaxSize must be <= 256MB")
	ErrAvgMaskOutOfRange = errors.New("AvgSize produces unsupported mask width")
)

const (
	minChunkSize = 64 * 1024
	maxChunkSize = 256 * 1024 * 1024
)

// Options holds parameters for FastCDC chunking.
type Options struct {
	MinSize int
	AvgSize int
	MaxSize int
}

// DefaultOptions returns standard 1MB Min, 4MB Avg, 8MB Max defaults.
func DefaultOptions() Options {
	return Options{
		MinSize: 1 * 1024 * 1024,
		AvgSize: 4 * 1024 * 1024,
		MaxSize: 8 * 1024 * 1024,
	}
}

type internalConfig struct {
	Options
	maskStrict uint64
	maskRelax  uint64
}

func (o Options) validateAndBuild() (internalConfig, error) {
	if o.MinSize < minChunkSize {
		return internalConfig{}, ErrInvalidMinSize
	}
	if o.AvgSize <= o.MinSize {
		return internalConfig{}, ErrInvalidAvgSize
	}
	if o.MaxSize <= o.AvgSize {
		return internalConfig{}, ErrInvalidMaxSize
	}
	if o.MaxSize > maxChunkSize {
		return internalConfig{}, ErrMaxSizeTooLarge
	}

	avg := uint64(o.AvgSize)
	floor := uint(bits.Len64(avg) - 1)
	rounded := floor
	if floor < 63 {
		lower := uint64(1) << floor
		upper := lower << 1
		if avg-lower >= upper-avg {
			rounded++
		}
	}
	if rounded < 1 || rounded > 62 {
		return internalConfig{}, ErrAvgMaskOutOfRange
	}

	// Dual normalization: Mask 1 requires +1 zero bits (stricter),
	// Mask 2 requires -1 zero bits (relaxed).
	maskStrict := (uint64(1) << (rounded + 1)) - 1
	maskRelax := (uint64(1) << (rounded - 1)) - 1

	return internalConfig{
		Options:    o,
		maskStrict: maskStrict,
		maskRelax:  maskRelax,
	}, nil
}
