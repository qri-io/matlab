// Package matlab defines readers & writers for working with matlab .mat files
package matlab

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// DataType represents matlab data types
type DataType uint32

func (d DataType) String() string {
	switch d {
	case DTmiINT8:
		return "miINT8"
	case DTmiUINT8:
		return "miUINT8"
	case DTmiINT16:
		return "miINT16"
	case DTmiUINT16:
		return "miUINT16"
	case DTmiINT32:
		return "miINT32"
	case DTmiUINT32:
		return "miUINT32"
	case DTmiSINGLE:
		return "miSINGLE"
	case DTmiDOUBLE:
		return "miDOUBLE"
	case DTmiINT64:
		return "miINT64"
	case DTmiUINT64:
		return "miUINT64"
	case DTmiMATRIX:
		return "miMATRIX"
	case DTmiCOMPRESSED:
		return "miCOMPRESSED"
	case DTmiUTF8:
		return "miUTF8"
	case DTmiUTF16:
		return "miUTF16"
	case DTmiUTF32:
		return "miUTF32"
	default:
		return "unknown"
	}
}

// Data Types as specified according to byte indicators
const (
	DataTypeUnknown DataType = iota // errored data type
	DTmiINT8                        // 8 bit, signed
	DTmiUINT8                       // 8 bit, unsigned
	DTmiINT16                       // 16-bit, signed
	DTmiUINT16                      // 16-bit, unsigned
	DTmiINT32                       // 32-bit, signed
	DTmiUINT32                      // 32-bit, unsigned
	DTmiSINGLE                      // IEEE® 754 single format
	_
	DTmiDOUBLE // IEEE 754 double format
	_
	_
	DTmiINT64      // 64-bit, signed
	DTmiUINT64     // 64-bit, unsigned
	DTmiMATRIX     // MATLAB array
	DTmiCOMPRESSED // Compressed Data
	DTmiUTF8       // Unicode UTF-8 Encoded Character Data
	DTmiUTF16      // Unicode UTF-16 Encoded Character Data
	DTmiUTF32      // Unicode UTF-32 Encoded Character Data
)

// File represents a .mat matlab file
type File struct {
	Header *Header
	r      io.Reader
	w      io.Writer
}

// Header is a matlab .mat file header
type Header struct {
	Level     string
	Platform  string
	Created   time.Time
	Endianess binary.ByteOrder
}

// String implements the stringer interface for Header
// with the standard .mat file prefix (without the filler bytes)
func (h *Header) String() string {
	return fmt.Sprintf("MATLAB %s MAT-file, Platform: %s, Created on: %s", h.Level, h.Platform, h.Created.Format(time.ANSIC))
}

// Element is a parsed matlab data element
type Element struct {
	Type  DataType
	Value interface{}
}

// NewFileFromReader creates a file from a reader and attempts to read
// the header
func NewFileFromReader(r io.Reader) (f *File, err error) {
	f = &File{r: r}
	err = f.readHeader()
	return
}

const (
	headerLen                = 128
	headerTextLen            = 116
	headerSubsystemOffsetLen = 8
	headerFlagLen            = 4
)

func (f *File) readHeader() (err error) {
	var buf []byte
	h := &Header{}
	f.Header = h

	// read description
	if buf, err = readAllBytes(headerTextLen, f.r); err != nil {
		return
	}

	r := bufio.NewReader(bytes.NewBuffer(buf))

	if prefix, err := r.ReadBytes(' '); err != nil {
		return err
	} else if !bytes.Equal(prefix, []byte("MATLAB ")) {
		return fmt.Errorf("not a valid .mat file")
	}

	if h.Level, err = r.ReadString(' '); err != nil {
		return err
	}

	h.Level = strings.TrimSpace(h.Level)
	if h.Level != "5.0" {
		return fmt.Errorf("can only read matlab level 5 files")
	}

	if _, err = r.Discard(len("MAT-file Platform: ")); err != nil {
		return
	}

	if h.Platform, err = r.ReadString(','); err != nil {
		return
	}
	h.Platform = strings.TrimRight(h.Platform, ",")

	if _, err = r.Discard(len(" Created on: ")); err != nil {
		return
	}

	date := make([]byte, 24)
	if _, err = r.Read(date); err != nil {
		return
	}
	if h.Created, err = time.Parse(time.ANSIC, strings.TrimSpace(string(date))); err != nil {
		return
	}

	if _, err = readAllBytes(headerSubsystemOffsetLen, f.r); err != nil {
		return
	}

	if buf, err = readAllBytes(headerFlagLen, f.r); err != nil {
		return
	}

	byteOrder := string(buf[2:4])
	if byteOrder == "MI" {
		h.Endianess = binary.BigEndian
	} else if byteOrder == "IM" {
		h.Endianess = binary.LittleEndian
	} else {
		return fmt.Errorf("invalid byte order setting: %s", byteOrder)
	}

	return nil
}

func readAllBytes(p int, rdr io.Reader) (buf []byte, err error) {
	var (
		n int
		r []byte
	)

	for p > 0 {
		r = make([]byte, p)
		n, err = rdr.Read(r)

		if err != nil {
			return
		}

		buf = append(buf, r[:n]...)
		p -= n
	}
	return
}

func (f *File) readUint32() (uint32, error) {
	buf, err := readAllBytes(4, f.r)
	if err != nil {
		return uint32(0), err
	}
	return f.Header.Endianess.Uint32(buf), nil
}

// ReadElement reads a single Element from a file's reader
func (f *File) ReadElement() (el *Element, err error) {
	return readElement(f.r, f.Header.Endianess)
}

func readElement(r io.Reader, bo binary.ByteOrder) (el *Element, err error) {
	buf, err := readAllBytes(4, r)
	if err != nil {
		return nil, err
	}

	// handle small type
	if buf[0] != 0 && buf[1] != 0 {
		el = &Element{Type: DataType(bo.Uint16(buf[1:]))}
		if _, err = r.Read(buf); err != nil {
			return
		}
		fmt.Println("SMOL", el.Type.String(), hex.EncodeToString(buf))
		el.Value, err = parse(el.Type, bo, buf)
		return
	}

	el = &Element{Type: DataType(bo.Uint32(buf))}
	// read number of bytes, we know buf is 4 bytes long so this works
	if _, err = r.Read(buf); err != nil {
		return nil, err
	}
	p := bo.Uint32(buf)

	// TODO - remove
	fmt.Println(el.Type.String(), p)

	if el.Type != DTmiCOMPRESSED {
		// read data
		if buf, err = readAllBytes(int(p), r); err != nil {
			return nil, err
		}
	} else {
		// data is compressed, use zlib reader
		cr, err := zlib.NewReader(r)
		if err != nil {
			return nil, err
		}
		if buf, err = readAllBytes(int(p), cr); err != nil {
			return nil, err
		}
		if err = cr.Close(); err != nil {
			return nil, err
		}

		return readElement(bytes.NewBuffer(buf), bo)
	}

	el.Value, err = parse(el.Type, bo, buf)
	return
}

func parse(t DataType, bo binary.ByteOrder, data []byte) (interface{}, error) {
	switch t {
	case DTmiMATRIX:
		return miMatrix(bo, data)
	case DTmiUINT32:
		return bo.Uint32(data), nil
	case DTmiINT32:
		return int32(bo.Uint32(data)), nil
	default:
		return nil, fmt.Errorf("cannot parse data type: %s", t)
	}
}

func miMatrix(bo binary.ByteOrder, data []byte) (interface{}, error) {
	buf := bytes.NewBuffer(data)
	var els []interface{}
	for {
		el, err := readElement(buf, bo)
		if err != nil {
			return nil, err
		}
		els = append(els, el)
	}
	return nil, fmt.Errorf("cannot parse miMatrix")
}

func writeHeader(w io.Writer, h *Header) error {
	return fmt.Errorf("not finished")
}

// WriteElement writes a single element to a file's writer
func (f *File) WriteElement(e *Element) error {
	return fmt.Errorf("not finished")
}
