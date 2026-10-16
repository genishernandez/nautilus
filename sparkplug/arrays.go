package sparkplug

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/joyautomation/nautilus/sparkplug/spb"
)

// Sparkplug B array datatypes (Int8Array … DateTimeArray, enum 22–34) and
// their wire encoding.
//
// Eclipse Sparkplug 3.0.0, §6.4.17 "Datatype Details" › "Array Types":
// "All array types use the bytes_value field of the Metric value field. They
// are simply little-endian packed byte arrays." Per type:
//
//	Int8/16/32/64Array    packed little-endian two's-complement, 1/2/4/8 bytes
//	UInt8/16/32/64Array   packed little-endian unsigned, 1/2/4/8 bytes
//	FloatArray            packed little-endian IEEE 754 binary32
//	DoubleArray           packed little-endian IEEE 754 binary64
//	BooleanArray          "bit-packed bytes preceded by a 4-byte integer that
//	                      represents the total number of boolean values"
//	StringArray           "an array of null terminated strings"
//	DateTimeArray         "each Datetime value is an 8-byte value representing
//	                      the number of milliseconds since epoch in UTC"
//
// Where the spec leaves a detail to its examples, Eclipse Tahu's Java
// reference encoder/decoder (SparkplugBPayloadEncoder/Decoder) settles it,
// and the two agree:
//
//   - BooleanArray's count is a little-endian uint32 (ByteBuffer LITTLE_ENDIAN
//     putInt), and bits are MSB-first: element i is bit 7-(i%8) of byte i/8
//     (encoder `nextByte |= (128 >> bit)`, decoder `1 << (7 - j)`; the spec's
//     example [f,f,t,t,f,t,f,f,…] → 0x34 says the same). Padding bits are 0.
//   - StringArray is UTF-8 with a 0x00 after EVERY element, the last included
//     (spec example [ABC, hello] → 41 42 43 00 68 65 6c 6c 6f 00).
//   - DateTimeArray elements are little-endian int64 (Tahu putLong/getLong).
//
// The spec's own FloatArray/DoubleArray example bytes are printed big-endian,
// against its text; the text and Tahu (LITTLE_ENDIAN) are what this follows.
//
// Tahu's Python array_packer is known to differ (LSB-first booleans,
// hex-text strings); it interoperates with nothing but itself, and nautilus
// does not try to.
//
// In a Metric, an array's Value is a Go slice: []bool (BooleanArray), []int64
// (every integer array and DateTimeArray — the IR's integer, the same as the
// scalar mapping), []float64 (FloatArray, DoubleArray) or []string
// (StringArray). Unsigned 64-bit elements above 2^63 wrap, as UInt64 scalars do.

// isArrayType reports whether dt is one of the Sparkplug array datatypes.
func isArrayType(dt spb.DataType) bool {
	return dt >= spb.DataType_Int8Array && dt <= spb.DataType_DateTimeArray
}

// IsArrayDatatype reports whether dt is one of the Sparkplug array datatypes
// (Int8Array … DateTimeArray).
func IsArrayDatatype(dt spb.DataType) bool { return isArrayType(dt) }

// ArrayElem is the scalar datatype of an array datatype's elements
// (Int32Array → Int32, DateTimeArray → DateTime); ok is false for a
// non-array datatype.
func ArrayElem(dt spb.DataType) (spb.DataType, bool) {
	switch dt {
	case spb.DataType_Int8Array:
		return spb.DataType_Int8, true
	case spb.DataType_Int16Array:
		return spb.DataType_Int16, true
	case spb.DataType_Int32Array:
		return spb.DataType_Int32, true
	case spb.DataType_Int64Array:
		return spb.DataType_Int64, true
	case spb.DataType_UInt8Array:
		return spb.DataType_UInt8, true
	case spb.DataType_UInt16Array:
		return spb.DataType_UInt16, true
	case spb.DataType_UInt32Array:
		return spb.DataType_UInt32, true
	case spb.DataType_UInt64Array:
		return spb.DataType_UInt64, true
	case spb.DataType_FloatArray:
		return spb.DataType_Float, true
	case spb.DataType_DoubleArray:
		return spb.DataType_Double, true
	case spb.DataType_BooleanArray:
		return spb.DataType_Boolean, true
	case spb.DataType_StringArray:
		return spb.DataType_String, true
	case spb.DataType_DateTimeArray:
		return spb.DataType_DateTime, true
	}
	return spb.DataType_Unknown, false
}

// ArrayOf is the array datatype whose elements are dt (Int32 → Int32Array);
// ok is false when Sparkplug has no array of dt (Text, UUID, Template, …).
func ArrayOf(dt spb.DataType) (spb.DataType, bool) {
	for a := spb.DataType_Int8Array; a <= spb.DataType_DateTimeArray; a++ {
		if e, _ := ArrayElem(a); e == dt {
			return a, true
		}
	}
	return spb.DataType_Unknown, false
}

// intWidth is the element width in bytes of an integer array datatype, and
// whether it is signed. DateTimeArray is a signed 8-byte integer on the wire.
func intWidth(dt spb.DataType) (width int, signed bool) {
	switch dt {
	case spb.DataType_Int8Array:
		return 1, true
	case spb.DataType_Int16Array:
		return 2, true
	case spb.DataType_Int32Array:
		return 4, true
	case spb.DataType_Int64Array, spb.DataType_DateTimeArray:
		return 8, true
	case spb.DataType_UInt8Array:
		return 1, false
	case spb.DataType_UInt16Array:
		return 2, false
	case spb.DataType_UInt32Array:
		return 4, false
	case spb.DataType_UInt64Array:
		return 8, false
	}
	return 0, false
}

// encodeArray packs an array value into its bytes_value form. An element that
// does not fit its datatype (300 in an Int8Array, -1 in a UInt16Array) is an
// error, never a silent truncation.
func encodeArray(dt spb.DataType, v any) ([]byte, error) {
	if w, signed := intWidth(dt); w > 0 {
		xs, ok := v.([]int64)
		if !ok {
			return nil, typeErr("[]int64", v)
		}
		out := make([]byte, len(xs)*w)
		for i, x := range xs {
			if !fitsInt(x, w, signed) {
				return nil, fmt.Errorf("element %d (%d) does not fit %v", i, x, dt)
			}
			b := out[i*w:]
			switch w {
			case 1:
				b[0] = byte(x)
			case 2:
				binary.LittleEndian.PutUint16(b, uint16(x))
			case 4:
				binary.LittleEndian.PutUint32(b, uint32(x))
			case 8:
				binary.LittleEndian.PutUint64(b, uint64(x))
			}
		}
		return out, nil
	}
	switch dt {
	case spb.DataType_FloatArray, spb.DataType_DoubleArray:
		xs, ok := v.([]float64)
		if !ok {
			return nil, typeErr("[]float64", v)
		}
		if dt == spb.DataType_FloatArray {
			out := make([]byte, len(xs)*4)
			for i, x := range xs {
				binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(float32(x)))
			}
			return out, nil
		}
		out := make([]byte, len(xs)*8)
		for i, x := range xs {
			binary.LittleEndian.PutUint64(out[i*8:], math.Float64bits(x))
		}
		return out, nil
	case spb.DataType_BooleanArray:
		xs, ok := v.([]bool)
		if !ok {
			return nil, typeErr("[]bool", v)
		}
		out := make([]byte, 4+(len(xs)+7)/8)
		binary.LittleEndian.PutUint32(out, uint32(len(xs)))
		for i, x := range xs {
			if x {
				out[4+i/8] |= 0x80 >> (i % 8) // MSB-first
			}
		}
		return out, nil
	case spb.DataType_StringArray:
		xs, ok := v.([]string)
		if !ok {
			return nil, typeErr("[]string", v)
		}
		var sb strings.Builder
		for i, s := range xs {
			if strings.IndexByte(s, 0) >= 0 {
				// The terminator is the only delimiter: an embedded NUL
				// would split the element in two on the far side.
				return nil, fmt.Errorf("element %d contains a NUL byte, which a StringArray cannot carry", i)
			}
			sb.WriteString(s)
			sb.WriteByte(0)
		}
		return []byte(sb.String()), nil
	}
	return nil, fmt.Errorf("unsupported datatype %v", dt)
}

// fitsInt reports whether x is representable in a w-byte integer. A UInt64
// element is any int64 bit pattern — the IR's int64 already carries the
// values above 2^63 wrapped, the same as a UInt64 scalar.
func fitsInt(x int64, w int, signed bool) bool {
	if w == 8 {
		return true
	}
	bits := uint(w * 8)
	if signed {
		lo, hi := int64(-1)<<(bits-1), int64(1)<<(bits-1)-1
		return x >= lo && x <= hi
	}
	return x >= 0 && x <= int64(1)<<bits-1
}

// decodeArray unpacks a bytes_value into the slice form for dt. A packed
// numeric array whose length is not a multiple of its element width, or a
// BooleanArray whose count claims more bits than it carries, is malformed —
// an error rather than a guess at what the sender meant.
func decodeArray(dt spb.DataType, b []byte) (any, error) {
	if w, signed := intWidth(dt); w > 0 {
		if len(b)%w != 0 {
			return nil, fmt.Errorf("%v: %d bytes is not a whole number of %d-byte elements", dt, len(b), w)
		}
		out := make([]int64, len(b)/w)
		for i := range out {
			e := b[i*w:]
			switch {
			case w == 1 && signed:
				out[i] = int64(int8(e[0]))
			case w == 1:
				out[i] = int64(e[0])
			case w == 2 && signed:
				out[i] = int64(int16(binary.LittleEndian.Uint16(e)))
			case w == 2:
				out[i] = int64(binary.LittleEndian.Uint16(e))
			case w == 4 && signed:
				out[i] = int64(int32(binary.LittleEndian.Uint32(e)))
			case w == 4:
				out[i] = int64(binary.LittleEndian.Uint32(e))
			default:
				out[i] = int64(binary.LittleEndian.Uint64(e))
			}
		}
		return out, nil
	}
	switch dt {
	case spb.DataType_FloatArray:
		if len(b)%4 != 0 {
			return nil, fmt.Errorf("%v: %d bytes is not a whole number of 4-byte elements", dt, len(b))
		}
		out := make([]float64, len(b)/4)
		for i := range out {
			out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:])))
		}
		return out, nil
	case spb.DataType_DoubleArray:
		if len(b)%8 != 0 {
			return nil, fmt.Errorf("%v: %d bytes is not a whole number of 8-byte elements", dt, len(b))
		}
		out := make([]float64, len(b)/8)
		for i := range out {
			out[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[i*8:]))
		}
		return out, nil
	case spb.DataType_BooleanArray:
		if len(b) == 0 {
			return []bool{}, nil // an absent bytes_value: Tahu reads it as no elements
		}
		if len(b) < 4 {
			return nil, fmt.Errorf("%v: %d bytes is shorter than the 4-byte count", dt, len(b))
		}
		n := binary.LittleEndian.Uint32(b)
		if uint64(n) > uint64(len(b)-4)*8 {
			return nil, fmt.Errorf("%v: count %d needs %d bytes of bits, have %d", dt, n, (uint64(n)+7)/8, len(b)-4)
		}
		out := make([]bool, n)
		for i := range out {
			out[i] = b[4+i/8]&(0x80>>(i%8)) != 0
		}
		return out, nil
	case spb.DataType_StringArray:
		out := []string{}
		for len(b) > 0 {
			i := strings.IndexByte(string(b), 0)
			if i < 0 {
				// The last element lacks its terminator. The spec says
				// every string has one; take the bytes anyway rather than
				// drop a value a lenient sender meant.
				out = append(out, string(b))
				break
			}
			out = append(out, string(b[:i]))
			b = b[i+1:]
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported datatype %v", dt)
}
