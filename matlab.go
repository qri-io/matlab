// Package matlab defines readers & writers for working with matlab .mat files
package matlab

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
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
			if err.Error() == "EOF" {
				if p-n == 0 {
					return append(buf, r[:n]...), nil
				}
			}
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
	return readElement(f.Header.Endianess, f.r)
}

func readElement(bo binary.ByteOrder, r io.Reader) (el *Element, err error) {
	var p int
	el, p, err = readTag(bo, r)

	// if small element, p will be 0, bail early
	if p == 0 {
		return
	}

	var buf []byte
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
		defer cr.Close()

		return readElement(bo, cr)
	}

	el.Value, err = parse(el.Type, bo, buf)
	return
}

func readTag(bo binary.ByteOrder, r io.Reader) (el *Element, len int, err error) {
	var buf []byte
	if buf, err = readAllBytes(8, r); err != nil {
		return
	}

	// handle small type
	if buf[0] != 0 && buf[1] != 0 {
		len = int(bo.Uint16(buf[:2]))
		el = &Element{Type: DataType(bo.Uint16(buf[1:3]))}
		fmt.Println("SMOL: ", el.Type.String(), len, buf)
		el.Value, err = parse(el.Type, bo, buf[3:])
		return
	}

	el = &Element{Type: DataType(bo.Uint32(buf[:4]))}
	len = int(bo.Uint32(buf[4:]))
	fmt.Println("read tag", el.Type.String(), len, buf)
	return
}

func parse(t DataType, bo binary.ByteOrder, data []byte) (interface{}, error) {
	switch t {
	case DTmiINT8:
		return int(data[0]), nil
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

func miMatrix(bo binary.ByteOrder, data []byte) (val interface{}, err error) {
	r := bytes.NewBuffer(data)

	complex, class, err := arrayFlags(bo, r)
	if err != nil {
		return
	}
	fmt.Println(complex, class.String())

	dim, err := dimensionsArray(bo, r)
	if err != nil {
		return
	}

	name, err := arrayName(bo, r)
	if err != nil {
		return
	}
	fmt.Println(name, dim)
	return nil, fmt.Errorf("not finished")

	// var els []interface{}
	// for {
	// 	el, err := readElement(bo, r)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	els = append(els, el)
	// }
	// return nil, fmt.Errorf("cannot parse miMatrix")
}

func arrayFlags(bo binary.ByteOrder, r io.Reader) (complex bool, class mxClass, err error) {
	fmt.Println("read array flags")
	el, p, err := readTag(bo, r)
	if el.Type != DTmiUINT32 {
		err = fmt.Errorf("invalid matrix")
		return
	}

	buf := make([]byte, p)
	// // read array flags
	if _, err = r.Read(buf); err != nil {
		return
	}
	// flags := (buf[0] &&& 0xff00) >>> 8 |> byte
	// complex, glbl, logical := flags &&& 8, flags &&& 4, flags &&& 2
	// fmt.Println(p, hex.EncodeToString(buf))
	// TODO -
	// complex = 8 & f[2]
	// fmt.Println(hex.EncodeToString(buf), uint8(buf[3]))
	fmt.Println(buf, buf[0]&0x00ff)
	class = mxClass(buf[0] & 0x00ff)
	return
}

func dimensionsArray(bo binary.ByteOrder, r io.Reader) ([]int32, error) {
	fmt.Println("dimensions array")
	el, p, err := readTag(bo, r)
	if err != nil {
		return nil, err
	}
	if el.Type != DTmiINT32 {
		return nil, fmt.Errorf("invalid data type")
	}

	// fmt.Println("NO MOAR TAGS", el.Type.String(), p)
	buf := make([]byte, p)
	if _, err := r.Read(buf); err != nil {
		return nil, err
	}

	dimsr := bytes.NewBuffer(buf)
	sBuf := make([]byte, 4)
	dim := make([]int32, p/4)
	for i := 0; i < p/4; i++ {
		if _, err := dimsr.Read(sBuf); err != nil {
			return nil, err
		}
		dim[i] = int32(bo.Uint32(sBuf))
	}
	fmt.Println(dim)
	return dim, nil
}

func arrayName(bo binary.ByteOrder, r io.Reader) (string, error) {
	fmt.Println("array name")
	_, p, err := readTag(bo, r)
	if err != nil {
		return "", err
	}

	// if el.Type != DTmiINT8 {
	// 	return "", fmt.Errorf("invalid data type")
	// }
	// dimsr := bytes.NewBuffer(buf)
	// sBuf := make([]byte, 4)
	// dim := make([]byte, p/4)
	// for i := 0; i < p/4; i++ {
	// 	if _, err := dimsr.Read(sBuf); err != nil {
	// 		return nil, err
	// 	}
	// 	dim[i] = int32(bo.Uint32(sBuf))
	// }
	data, err := readAllBytes(p, r)
	return string(data), err
}

type mxClass uint8

func (c mxClass) String() string {
	switch c {
	case mxCELL:
		return "Cell array"
	case mxSTRUCT:
		return "Structure"
	case mxOBJECT:
		return "Object"
	case mxCHAR:
		return "Character array"
	case mxSPARSE:
		return "Sparse array"
	case mxDOUBLE:
		return "Double precision array"
	case mxSINGLE:
		return "Single precision array"
	case mxINT8:
		return "8-bit, signed integer"
	case mxUINT8:
		return "8-bit, unsigned integer"
	case mxINT16:
		return "16-bit, signed integer"
	case mxUINT16:
		return "16-bit, unsigned integer"
	case mxINT32:
		return "32-bit, signed integer"
	case mxUINT32:
		return "32-bit, unsigned integer"
	case mxINT64:
		return "64-bit, signed integer"
	case mxUINT64:
		return "64-bit, unsigned integer"
	default:
		return "unknown"
	}
}

// MATLAB Array Types (Classes)
const (
	mxUNKNOWN mxClass = iota
	mxCELL            // Cell array
	mxSTRUCT          // Structure
	mxOBJECT          // Object
	mxCHAR            // Character array
	mxSPARSE          // Sparse array *NB: don't use*
	mxDOUBLE          // Double precision array
	mxSINGLE          // Single precision array
	mxINT8            // 8-bit, signed integer
	mxUINT8           // 8-bit, unsigned integer
	mxINT16           // 16-bit, signed integer
	mxUINT16          // 16-bit, unsigned integer
	mxINT32           // 32-bit, signed integer
	mxUINT32          // 32-bit, unsigned integer
	mxINT64           // 64-bit, signed integer
	mxUINT64          // 64-bit, unsigned integer
)

func writeHeader(w io.Writer, h *Header) error {
	return fmt.Errorf("not finished")
}

// WriteElement writes a single element to a file's writer
func (f *File) WriteElement(e *Element) error {
	return fmt.Errorf("not finished")
}
