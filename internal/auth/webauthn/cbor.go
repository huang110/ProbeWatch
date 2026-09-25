package webauthn

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	ErrCBOREOF         = errors.New("cbor: unexpected EOF")
	ErrCBORUnsupported = errors.New("cbor: unsupported data type")
)

// DecodeCBOR decodes a single CBOR data item from b and returns (value, consumedBytes, error).
func DecodeCBOR(b []byte) (any, int, error) {
	if len(b) == 0 {
		return nil, 0, ErrCBOREOF
	}
	header := b[0]
	major := header >> 5
	info := header & 0x1f

	offset := 1
	var val uint64

	switch {
	case info < 24:
		val = uint64(info)
	case info == 24:
		if len(b) < offset+1 {
			return nil, 0, ErrCBOREOF
		}
		val = uint64(b[offset])
		offset++
	case info == 25:
		if len(b) < offset+2 {
			return nil, 0, ErrCBOREOF
		}
		val = uint64(binary.BigEndian.Uint16(b[offset : offset+2]))
		offset += 2
	case info == 26:
		if len(b) < offset+4 {
			return nil, 0, ErrCBOREOF
		}
		val = uint64(binary.BigEndian.Uint32(b[offset : offset+4]))
		offset += 4
	case info == 27:
		if len(b) < offset+8 {
			return nil, 0, ErrCBOREOF
		}
		val = binary.BigEndian.Uint64(b[offset : offset+8])
		offset += 8
	default:
		return nil, 0, ErrCBORUnsupported
	}

	switch major {
	case 0: // Unsigned integer
		return int64(val), offset, nil
	case 1: // Negative integer: -1 - val
		return -1 - int64(val), offset, nil
	case 2: // Byte string
		length := int(val)
		if len(b) < offset+length {
			return nil, 0, ErrCBOREOF
		}
		res := make([]byte, length)
		copy(res, b[offset:offset+length])
		return res, offset + length, nil
	case 3: // Text string
		length := int(val)
		if len(b) < offset+length {
			return nil, 0, ErrCBOREOF
		}
		return string(b[offset : offset+length]), offset + length, nil
	case 4: // Array
		count := int(val)
		items := make([]any, count)
		for i := 0; i < count; i++ {
			item, n, err := DecodeCBOR(b[offset:])
			if err != nil {
				return nil, 0, err
			}
			items[i] = item
			offset += n
		}
		return items, offset, nil
	case 5: // Map
		count := int(val)
		m := make(map[any]any, count)
		for i := 0; i < count; i++ {
			key, n1, err := DecodeCBOR(b[offset:])
			if err != nil {
				return nil, 0, err
			}
			offset += n1
			valItem, n2, err := DecodeCBOR(b[offset:])
			if err != nil {
				return nil, 0, err
			}
			offset += n2
			m[key] = valItem
		}
		return m, offset, nil
	case 6: // Semantic tag
		// Skip tag and return tagged item
		item, n, err := DecodeCBOR(b[offset:])
		if err != nil {
			return nil, 0, err
		}
		return item, offset + n, nil
	case 7: // Simple / Float
		switch info {
		case 20:
			return false, offset, nil
		case 21:
			return true, offset, nil
		case 22:
			return nil, offset, nil
		default:
			return nil, offset, nil
		}
	default:
		return nil, 0, fmt.Errorf("cbor: unhandled major type %d", major)
	}
}
