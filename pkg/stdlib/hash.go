package stdlib

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerHash registers hash.* functions.
func (r *Registry) registerHash() {
	r.Register("hash.compute_checksum", hashComputeChecksum)
	r.Register("hash.compute_hmac", hashComputeHMAC)
}

func hashComputeChecksum(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("hash.compute_checksum requires arguments")
	}

	var data []byte
	var algorithm string

	if args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("data"); ok {
			data = toBytes(v)
		}
		if v, ok := m.Get("algorithm"); ok && v.Type() == types.TypeString {
			algorithm = v.AsString()
		}
	} else if len(args) >= 2 {
		data = toBytes(args[0])
		algorithm = args[1].AsString()
	}

	h, err := newHash(algorithm)
	if err != nil {
		return types.Null, err
	}

	h.Write(data)
	return types.NewBytes(h.Sum(nil)), nil
}

func hashComputeHMAC(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("hash.compute_hmac requires arguments")
	}

	var data, key []byte
	var algorithm string

	if args[0].Type() == types.TypeMap {
		m := args[0].AsMap()
		if v, ok := m.Get("data"); ok {
			data = toBytes(v)
		}
		if v, ok := m.Get("key"); ok {
			key = toBytes(v)
		}
		if v, ok := m.Get("algorithm"); ok && v.Type() == types.TypeString {
			algorithm = v.AsString()
		}
	} else if len(args) >= 3 {
		data = toBytes(args[0])
		key = toBytes(args[1])
		algorithm = args[2].AsString()
	}

	hashFunc, err := hashFactory(algorithm)
	if err != nil {
		return types.Null, err
	}

	mac := hmac.New(hashFunc, key)
	mac.Write(data)
	return types.NewBytes(mac.Sum(nil)), nil
}

func toBytes(v types.Value) []byte {
	switch v.Type() {
	case types.TypeBytes:
		return v.AsBytes()
	case types.TypeString:
		return []byte(v.AsString())
	default:
		return []byte(v.String())
	}
}

func newHash(algorithm string) (hash.Hash, error) {
	switch algorithm {
	case "SHA256":
		return sha256.New(), nil
	case "SHA384":
		return sha512.New384(), nil
	case "SHA512":
		return sha512.New(), nil
	case "MD5":
		return md5.New(), nil
	case "SHA1":
		return sha1.New(), nil
	default:
		return nil, types.NewValueError(
			fmt.Sprintf("unsupported hash algorithm: %s", algorithm))
	}
}

func hashFactory(algorithm string) (func() hash.Hash, error) {
	switch algorithm {
	case "SHA256":
		return sha256.New, nil
	case "SHA384":
		return sha512.New384, nil
	case "SHA512":
		return sha512.New, nil
	case "MD5":
		return md5.New, nil
	case "SHA1":
		return sha1.New, nil
	default:
		return nil, types.NewValueError(
			fmt.Sprintf("unsupported HMAC algorithm: %s", algorithm))
	}
}
