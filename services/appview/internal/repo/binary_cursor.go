package repo

import "math"

// binaryCursor reads big-endian integers, raw spans and unsigned LEB128 varints.
type binaryCursor struct {
	bytes  []byte
	offset int
}

func newBinaryCursor(data []byte) *binaryCursor { return &binaryCursor{bytes: data} }

func (c *binaryCursor) remaining() int { return len(c.bytes) - c.offset }

func (c *binaryCursor) read(count int) ([]byte, error) {
	if count < 0 || count > c.remaining() {
		return nil, Invalid("truncated bytes")
	}
	out := c.bytes[c.offset : c.offset+count]
	c.offset += count
	return out, nil
}

func (c *binaryCursor) integer(count int) (uint64, error) {
	raw, err := c.read(count)
	if err != nil {
		return 0, err
	}
	var value uint64
	for _, b := range raw {
		value = value<<8 | uint64(b)
	}
	return value, nil
}

// varint decodes a canonical (minimally encoded) unsigned varint that fits in an int.
func (c *binaryCursor) varint() (int, error) {
	start := c.offset
	var value uint64
	for shift := 0; shift < 64; shift += 7 {
		b, err := c.integer(1)
		if err != nil {
			return 0, err
		}
		if !(shift < 63 || b <= 1) {
			return 0, Invalid("varint overflow")
		}
		value |= (b & 127) << shift
		if b&128 == 0 {
			if value > math.MaxInt64 || !(c.offset-start == 1 || b != 0) {
				return 0, Invalid("noncanonical varint")
			}
			return int(value), nil
		}
	}
	return 0, Invalid("varint overflow")
}
