package repo

import (
	"bytes"
	"crypto/sha256"
)

const cidBase32Alphabet = "abcdefghijklmnopqrstuvwxyz234567"

// CID is a binary CIDv1 restricted to ATProto's shape: raw (0x55) or DAG-CBOR
// (0x71) codec with a SHA-256 multihash. It is comparable and usable as a map key.
type CID [36]byte

// ParseCIDBytes validates and copies a 36-byte binary CID.
func ParseCIDBytes(b []byte) (CID, error) {
	var cid CID
	if len(b) != 36 || b[0] != 1 || (b[1] != 0x55 && b[1] != 0x71) || b[2] != 0x12 || b[3] != 0x20 {
		return cid, Invalid("invalid ATProto CID")
	}
	copy(cid[:], b)
	return cid, nil
}

// ParseCID decodes a canonical base32 (multibase "b") CID string.
func ParseCID(text string) (CID, error) {
	if len(text) != 59 || text[0] != 'b' {
		return CID{}, Invalid("invalid CID")
	}
	output := make([]byte, 0, 36)
	var buffer uint32
	bits := 0
	for i := 1; i < len(text); i++ {
		value := bytes.IndexByte([]byte(cidBase32Alphabet), text[i])
		if value < 0 {
			return CID{}, Invalid("invalid CID alphabet")
		}
		buffer = buffer<<5 | uint32(value)
		bits += 5
		if bits >= 8 {
			bits -= 8
			output = append(output, byte(buffer>>bits))
			buffer &= (1 << bits) - 1
		}
	}
	if buffer != 0 {
		return CID{}, Invalid("noncanonical CID")
	}
	return ParseCIDBytes(output)
}

// HashCID returns the DAG-CBOR SHA-256 CID of data.
func HashCID(data []byte) CID {
	var cid CID
	cid[0], cid[1], cid[2], cid[3] = 1, 0x71, 0x12, 0x20
	sum := sha256.Sum256(data)
	copy(cid[4:], sum[:])
	return cid
}

// IsDAGCBOR reports whether the CID uses the DAG-CBOR codec.
func (c CID) IsDAGCBOR() bool { return c[1] == 0x71 }

// Bytes returns the binary CID.
func (c CID) Bytes() []byte { return append([]byte(nil), c[:]...) }

// String encodes the CID as lowercase base32 with the "b" multibase prefix.
func (c CID) String() string {
	output := make([]byte, 1, 59)
	output[0] = 'b'
	var buffer uint32
	bits := 0
	for _, b := range c {
		buffer = buffer<<8 | uint32(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			output = append(output, cidBase32Alphabet[buffer>>bits&31])
		}
	}
	if bits > 0 {
		output = append(output, cidBase32Alphabet[buffer<<(5-bits)&31])
	}
	return string(output)
}

// Verify checks that data hashes to this CID's digest.
func (c CID) Verify(data []byte) error {
	sum := sha256.Sum256(data)
	if !bytes.Equal(sum[:], c[4:]) {
		return Invalid("block hash mismatch")
	}
	return nil
}
