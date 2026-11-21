package repo

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"math/big"
	"unicode/utf8"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	k256ecdsa "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

const (
	multicodecSecp256k1 = 0xe7
	multicodecP256      = 0x1200
	base58Alphabet      = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
)

// p256HalfOrder is floor(n/2) for P-256; ATProto requires low-S signatures.
var p256HalfOrder = []byte{
	0x7f, 0xff, 0xff, 0xff, 0x80, 0x00, 0x00, 0x00, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	0xde, 0x73, 0x7d, 0x56, 0xd3, 0x8b, 0xcf, 0x42, 0x79, 0xdc, 0xe5, 0x61, 0x7e, 0x31, 0x92, 0xa8,
}

// SigningKey is an ATProto Multikey public key (secp256k1 or P-256, compressed).
type SigningKey struct {
	Codec      int
	Compressed []byte
}

// ParseSigningKey decodes a base58btc ("z") multibase Multikey and validates the point.
func ParseSigningKey(multibase string) (SigningKey, error) {
	if multibase == "" || multibase[0] != 'z' || utf8.RuneCountInString(multibase) > 128 {
		return SigningKey{}, Invalid("invalid signing multibase")
	}
	input := []byte(multibase[1:])
	var little []byte
	for _, character := range input {
		digit := bytes.IndexByte([]byte(base58Alphabet), character)
		if digit < 0 {
			return SigningKey{}, Invalid("invalid signing key")
		}
		carry := digit
		for i := range little {
			carry += int(little[i]) * 58
			little[i] = byte(carry & 255)
			carry >>= 8
		}
		for carry > 0 {
			little = append(little, byte(carry&255))
			carry >>= 8
		}
	}
	leadingZeros := 0
	for leadingZeros < len(input) && input[leadingZeros] == '1' {
		leadingZeros++
	}
	decoded := make([]byte, leadingZeros, leadingZeros+len(little))
	for i := len(little) - 1; i >= 0; i-- {
		decoded = append(decoded, little[i])
	}
	reader := newBinaryCursor(decoded)
	codec, err := reader.varint()
	if err != nil {
		return SigningKey{}, err
	}
	compressed, _ := reader.read(reader.remaining())
	if (codec != multicodecSecp256k1 && codec != multicodecP256) || len(compressed) != 33 {
		return SigningKey{}, Invalid("unsupported signing curve")
	}
	key := SigningKey{Codec: codec, Compressed: append([]byte(nil), compressed...)}
	if codec == multicodecSecp256k1 {
		if _, err := secp256k1.ParsePubKey(key.Compressed); err != nil {
			return SigningKey{}, err
		}
	} else if _, err := key.p256PublicKey(); err != nil {
		return SigningKey{}, err
	}
	return key, nil
}

// Equal reports whether two keys are identical.
func (k SigningKey) Equal(other SigningKey) bool {
	return k.Codec == other.Codec && bytes.Equal(k.Compressed, other.Compressed)
}

func (k SigningKey) p256PublicKey() (*ecdsa.PublicKey, error) {
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), k.Compressed)
	if x == nil {
		return nil, Invalid("invalid P-256 public key")
	}
	uncompressed := make([]byte, 65)
	uncompressed[0] = 4
	x.FillBytes(uncompressed[1:33])
	y.FillBytes(uncompressed[33:])
	return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), uncompressed)
}

// Verify checks a 64-byte compact (r||s) low-S ECDSA signature over SHA-256(message).
func (k SigningKey) Verify(signature, message []byte) error {
	if len(signature) != 64 {
		return Invalid("invalid compact signature")
	}
	digest := sha256.Sum256(message)
	valid := false
	if k.Codec == multicodecSecp256k1 {
		key, err := secp256k1.ParsePubKey(k.Compressed)
		if err != nil {
			return err
		}
		var r, s secp256k1.ModNScalar
		if r.SetByteSlice(signature[:32]) || s.SetByteSlice(signature[32:]) {
			return Invalid("invalid compact signature")
		}
		// libsecp256k1 rejects non-normalized (high-S) signatures during verification.
		valid = !r.IsZero() && !s.IsZero() && !s.IsOverHalfOrder() &&
			k256ecdsa.NewSignature(&r, &s).Verify(digest[:], key)
	} else {
		// Go's verifier accepts either S normalization. ATProto requires
		// low-S, so check the fixed-width integer before verifying the digest.
		if bytes.Compare(p256HalfOrder, signature[32:]) < 0 {
			return Invalid("high-S signature")
		}
		key, err := k.p256PublicKey()
		if err != nil {
			return err
		}
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		valid = ecdsa.Verify(key, digest[:], r, s)
	}
	if !valid {
		return Invalid("invalid commit signature")
	}
	return nil
}
