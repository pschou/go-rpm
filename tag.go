package rpm

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const tagSize = 16

type tagHeader struct {
	Tag    TagType
	Type   uint32
	Offset uint32
	Count  uint32
}

type tagData interface {
	io.ReaderFrom
	io.WriterTo
	Len() int
}

type Tag struct {
	tagHeader
	idx  int
	off  int
	data tagData
}

func (t *Tag) writeHeader(w io.Writer) error {
	return binary.Write(w, binary.BigEndian, t.tagHeader)
}

func (t *Tag) StringSig() string {
	return t.string(true)
}

func (t *Tag) String() string {
	return t.string(false)
}

func (t *Tag) string(sig bool) string {
	var tt string
	switch t.Type {
	case RPM_INT8_TYPE:
		tt = "int8"
	case RPM_INT16_TYPE:
		tt = "int16"
	case RPM_INT32_TYPE:
		tt = "int32"
	case RPM_INT64_TYPE:
		tt = "int64"
	case RPM_CHAR_TYPE:
		tt = "char"
	case RPM_BIN_TYPE:
		tt = "bin"
	case RPM_I18NSTRING_TYPE:
		tt = "i18n"
	case RPM_STRING_TYPE:
		tt = "str"
	case RPM_STRING_ARRAY_TYPE:
		tt = "[]str"
	default:
		tt = "unknown(0x" + strconv.FormatUint(uint64(t.Type), 16) + ")"
	}
	s := t.Tag.String()
	// TODO: something else, signature and payload tags overlap
	if sig {
		if v, ok := sigTagString[t.Tag]; ok {
			s = v
		}
	}
	return fmt.Sprintf("%s, %d, %d, 0x%x, %s", s, t.Tag, t.Count, t.Offset, tt)
}

var errTagType = errors.New("rpm: invalid tag type")

func marshal(ok bool, v interface{}) ([]byte, error) {
	if !ok {
		return nil, errTagType
	}
	return json.Marshal(v)
}

func (t *Tag) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(&t.tagHeader)
	if err != nil {
		return nil, err
	}

	var jb []byte
	switch t.Type {
	case
		RPM_STRING_TYPE,
		RPM_I18NSTRING_TYPE,
		RPM_STRING_ARRAY_TYPE:
		r, ok := t.StringArray()
		jb, err = marshal(ok, struct{ Data []string }{r})
	case RPM_INT16_TYPE:
		r, ok := t.Int16()
		jb, err = marshal(ok, struct{ Data []uint16 }{r})
	case RPM_INT32_TYPE:
		r, ok := t.Int32()
		jb, err = marshal(ok, struct{ Data []uint32 }{r})
	case RPM_INT64_TYPE:
		r, ok := t.Int64()
		jb, err = marshal(ok, struct{ Data []uint64 }{r})
	default:
		r, ok := t.Bytes()
		jb, err = marshal(ok, struct{ Data []byte }{r})
	}
	if err != nil {
		return nil, err
	}

	b[len(b)-1] = ','
	return append(b, jb[1:]...), nil
}

type jsonTag struct {
	tagHeader
	Data json.RawMessage
}

func (t *Tag) UnmarshalJSON(b []byte) (err error) {
	r := new(jsonTag)
	if err = json.Unmarshal(b, r); err != nil {
		return err
	}
	t.tagHeader = r.tagHeader
	switch t.Type {
	case
		RPM_STRING_TYPE,
		RPM_I18NSTRING_TYPE,
		RPM_STRING_ARRAY_TYPE:
		var data []string
		err = json.Unmarshal(r.Data, &data)
		t.data = &tagString{data: data}
	case RPM_INT16_TYPE:
		var data tagUint16
		err = json.Unmarshal(r.Data, &data)
		t.data = data
	case RPM_INT32_TYPE:
		var data tagUint32
		err = json.Unmarshal(r.Data, &data)
		t.data = data
	case RPM_INT64_TYPE:
		var data tagUint64
		err = json.Unmarshal(r.Data, &data)
		t.data = data
	default:
		var data []byte
		err = json.Unmarshal(r.Data, &data)
		t.data = &tagBytes{b: bytes.NewBuffer(data)}
	}
	return err
}

type tagBytes struct {
	b     *bytes.Buffer
	count uint32
}

func (t *tagBytes) WriteTo(w io.Writer) (int64, error) {
	b := *t.b
	return b.WriteTo(w)
}

func (t *tagBytes) ReadFrom(w io.Reader) (int64, error) {
	if t.b == nil {
		t.b = new(bytes.Buffer)
	}
	n, err := t.b.ReadFrom(w)
	if err != nil {
		return 0, err
	}
	if n < int64(t.count) {
		return 0, errUnexpectedEOF
	}
	t.b.Truncate(int(t.count))
	return n, nil
}

func (t *tagBytes) Len() int {
	return t.b.Len()
}

type tagString struct {
	data []string
	len  int
}

func (t *tagString) WriteTo(w io.Writer) (int64, error) {
	var b int64
	for _, v := range t.data {
		n, err := io.WriteString(w, v+string(0))
		if err != nil {
			return b, err
		}
		b += int64(n)
	}
	return b, nil
}

func (t *tagString) ReadFrom(r io.Reader) (n int64, err error) {
	var sb []byte
	b := bufio.NewReader(r)
	for i := 0; i < len(t.data); i++ {
		if sb, err = b.ReadBytes(0); err != nil {
			return 0, err
		}
		t.len += len(sb)
		t.data[i] = string(sb[:len(sb)-1])
	}
	return int64(b.Buffered() + t.len), nil
}

func (t *tagString) Len() int {
	if t.len != 0 {
		return t.len
	}
	for _, v := range t.data {
		t.len += len(v) + 1
	}
	return t.len
}

func (t *Tag) StringData() (string, bool) {
	r, ok := t.data.(*tagString)
	if len(r.data) == 0 {
		return "", false
	}
	return r.data[0], ok
}

func (t *Tag) StringArray() ([]string, bool) {
	r, ok := t.data.(*tagString)
	return r.data, ok
}

type tagUint16 []uint16

func (t tagUint16) Len() int { return len(t) * 2 }
func (t tagUint16) WriteTo(w io.Writer) (int64, error) {
	return int64(t.Len()), binary.Write(w, binary.BigEndian, t)
}
func (t tagUint16) ReadFrom(r io.Reader) (int64, error) {
	return int64(t.Len()), binary.Read(r, binary.BigEndian, t)
}

func (t *Tag) Int16() ([]uint16, bool) {
	r, ok := t.data.(tagUint16)
	return r, ok
}

type tagUint32 []uint32

func (t tagUint32) Len() int { return len(t) * 4 }
func (t tagUint32) WriteTo(w io.Writer) (int64, error) {
	return int64(t.Len()), binary.Write(w, binary.BigEndian, t)
}
func (t tagUint32) ReadFrom(r io.Reader) (int64, error) {
	return int64(t.Len()), binary.Read(r, binary.BigEndian, t)
}

func (t *Tag) Int32() ([]uint32, bool) {
	r, ok := t.data.(tagUint32)
	return r, ok
}

type tagUint64 []uint64

func (t tagUint64) Len() int { return len(t) * 8 }
func (t tagUint64) WriteTo(w io.Writer) (int64, error) {
	return int64(t.Len()), binary.Write(w, binary.BigEndian, t)
}
func (t tagUint64) ReadFrom(r io.Reader) (int64, error) {
	return int64(t.Len()), binary.Read(r, binary.BigEndian, t)
}

func (t *Tag) Int64() ([]uint64, bool) {
	r, ok := t.data.(tagUint64)
	return r, ok
}

func (t *Tag) Bytes() ([]byte, bool) {
	switch r := t.data.(type) {
	case *bytes.Buffer:
		return r.Bytes(), true
	case *tagBytes:
		return r.b.Bytes(), true
	}
	return nil, false
}

var errTagSize = errors.New("rpm: invalid tag size")

func (t *Tag) make(a, b uint32) error {
	// TODO: remove padding
	dl := b - a
	switch t.Type {
	case RPM_INT16_TYPE:
		if t.Count > dl>>1 {
			return errTagSize
		}
		t.data = make(tagUint16, t.Count)
	case RPM_INT32_TYPE:
		if t.Count > dl>>2 {
			return errTagSize
		}
		t.data = make(tagUint32, t.Count)
	case RPM_INT64_TYPE:
		if t.Count > dl>>3 {
			return errTagSize
		}
		t.data = make(tagUint64, t.Count)
	case
		RPM_STRING_TYPE,
		RPM_I18NSTRING_TYPE,
		RPM_STRING_ARRAY_TYPE:
		// count is the number or null terminated strings
		// this only checks if its way off
		if t.Count > dl {
			return errTagSize
		}
		t.data = &tagString{data: make([]string, t.Count)}
	case
		RPM_BIN_TYPE,
		RPM_CHAR_TYPE,
		RPM_INT8_TYPE:
		if t.Count > dl {
			return errTagSize
		}
		t.data = &tagBytes{count: t.Count}
	default:
		return errTagType
	}
	return nil
}

func fprintf(w io.Writer, f string, ok bool, a ...interface{}) (int, error) {
	if !ok {
		return 0, errTagType
	}
	return fmt.Fprintf(w, f, a...)
}

func nl(w io.Writer, idx, n int, data string) (err error) {
	i := strings.IndexByte(data, '\n')
	if i == -1 {
		if n == 0 {
			_, err = fmt.Fprintf(w, "  %q\n", data)
		} else {
			_, err = fmt.Fprintf(w, " %4d:%q\n", idx, data)
		}
		return err
	}

	if _, err := fmt.Fprintf(w, " %4d:%q\n", idx, data[:i]); err != nil {
		return err
	}

	for _, v := range strings.Split(data[i+1:], "\n") {
		_, err := fmt.Fprintf(w, "      %q\n", v)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *Tag) DumpSignature(w io.Writer) error {
	return dump(w, t, true)
}

func (t *Tag) Dump(w io.Writer) error {
	return dump(w, t, false)
}

func dump(w io.Writer, tag *Tag, sig bool) (err error) {
	var s string
	if sig {
		s = tag.StringSig()
	} else {
		s = tag.String()
	}
	_, err = fmt.Fprintf(w, "0x%x: tag: %s", tag.off, s)
	if err != nil {
		return err
	}

	switch tag.Type {
	case RPM_INT8_TYPE:
		r, ok := tag.Bytes()
		_, err = fprintf(w, "\n  %x\n", ok, r)
	case RPM_CHAR_TYPE:
		r, ok := tag.Bytes()
		_, err = fprintf(w, "\n  %q\n", ok, r)
	case RPM_INT16_TYPE:
		r, ok := tag.Int16()
		_, err = fprintf(w, "\n  %x\n", ok, r)
	case RPM_INT32_TYPE:
		r, ok := tag.Int32()
		_, err = fprintf(w, "\n  %x\n", ok, r)
	case RPM_INT64_TYPE:
		r, ok := tag.Int64()
		_, err = fprintf(w, "\n  %x\n", ok, r)
	case RPM_BIN_TYPE:
		fmt.Fprintln(w)
		_, err = tag.data.WriteTo(hex.Dumper(w))
		fmt.Fprintln(w)
	case RPM_STRING_TYPE, RPM_I18NSTRING_TYPE:
		if r, ok := tag.StringData(); ok {
			fmt.Fprintln(w)
			err = nl(w, 0, 0, r)
		}
	case RPM_STRING_ARRAY_TYPE:
		r, ok := tag.StringArray()
		if !ok {
			break
		}
		fmt.Fprintln(w)

		if len(r) == 1 {
			err = nl(w, 0, 0, r[0])
			break
		}

		var (
			lv string
			li int
		)
		for i, v := range r {
			if lv == v && i > 0 {
				li++
				continue
			}
			if li > 0 {
				fmt.Fprintf(w, " %+4d\n", li)
			}
			if err = nl(w, i, 1, v); err != nil {
				break
			}
			lv = v
			li = 0
		}
		if li > 0 {
			fmt.Fprintf(w, " %+4d\n", li)
		}
	}
	return err
}
