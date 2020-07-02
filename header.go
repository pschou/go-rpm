package rpm

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sort"
)

var rpmHeaderMagic = [...]byte{
	// header entry magic
	0x8e, 0xad, 0xe8,
	// version
	0x1,
	// reserved
	0x0, 0x0, 0x0, 0x0,
}

type rpmHeaderPre struct {
	Magic [8]byte

	// tag count
	Count uint32

	// size of the tagdata section in bytes
	Length uint32
}

type Header struct {
	rpmHeaderPre
	off    uint32
	region *Tag
	Tags   []*Tag
}

func NewSignatureHeader() *Header {
	r := new(Header)
	r.SetRegion(HEADER_SIGNATURES)
	return r
}

func NewPayloadHeader() *Header {
	r := new(Header)
	r.SetRegion(HEADER_IMMUTABLE)
	return r
}

func (hdr *Header) Len() int { return len(hdr.Tags) }

func (hdr *Header) Swap(i, j int) {
	hdr.Tags[i], hdr.Tags[j] = hdr.Tags[j], hdr.Tags[i]
}

func (hdr *Header) Less(i, j int) bool {
	return hdr.Tags[i].Offset < hdr.Tags[j].Offset
}

func (hdr *Header) addString(tag TagType, t uint32, data string) error {
	return hdr.Add(&Tag{
		tagHeader: tagHeader{
			Tag:   tag,
			Type:  t,
			Count: 1,
		},
		data: &tagString{
			data: []string{data},
			len:  len(data) + 1,
		},
	})
}

func (hdr *Header) AddString(tag TagType, data string) error {
	return hdr.addString(tag, RPM_STRING_TYPE, data)
}

func (hdr *Header) AddStringI18N(tag TagType, data string) error {
	return hdr.addString(tag, RPM_I18NSTRING_TYPE, data)
}

func (hdr *Header) AddStringArray(tag TagType, data ...string) error {
	return hdr.Add(&Tag{
		tagHeader: tagHeader{
			Tag:   tag,
			Type:  RPM_STRING_ARRAY_TYPE,
			Count: uint32(len(data)),
		},
		data: &tagString{data: data},
	})
}

func (hdr *Header) AddInt16(tag TagType, data ...uint16) error {
	return hdr.Add(&Tag{
		tagHeader: tagHeader{
			Tag:   tag,
			Type:  RPM_INT16_TYPE,
			Count: uint32(len(data)),
		},
		data: tagUint16(data),
	})
}

func (hdr *Header) AddInt32(tag TagType, data ...uint32) error {
	return hdr.Add(&Tag{
		tagHeader: tagHeader{
			Tag:   tag,
			Type:  RPM_INT32_TYPE,
			Count: uint32(len(data)),
		},
		data: tagUint32(data),
	})
}

func (hdr *Header) AddInt64(tag TagType, data ...uint64) error {
	return hdr.Add(&Tag{
		tagHeader: tagHeader{
			Tag:   tag,
			Type:  RPM_INT64_TYPE,
			Count: uint32(len(data)),
		},
		data: tagUint64(data),
	})
}

func (hdr *Header) AddBin(tag TagType, data []byte) error {
	return hdr.Add(&Tag{
		tagHeader: tagHeader{
			Tag:   tag,
			Type:  RPM_BIN_TYPE,
			Count: uint32(len(data)),
		},
		data: &tagBytes{b: bytes.NewBuffer(data)},
	})
}

func (hdr *Header) SetRegion(tag TagType) {
	hdr.region = &Tag{
		tagHeader: tagHeader{
			Tag:   tag,
			Type:  RPM_BIN_TYPE,
			Count: tagSize,
		},
	}
}

func (hdr *Header) setRegion(pre *rpmHeaderPre) error {
	if hdr.region == nil {
		return nil
	}
	hdr.region.Offset = hdr.off
	pre.Length += tagSize
	pre.Count++

	data := new(bytes.Buffer)
	if err := binary.Write(data, binary.BigEndian, &tagHeader{
		Tag:    hdr.region.Tag,
		Type:   RPM_BIN_TYPE,
		Offset: uint32(-int32(len(hdr.Tags)+1) * tagSize),
		Count:  tagSize,
	}); err != nil {
		return err
	}

	hdr.region.data = &tagBytes{b: data}
	return nil
}

func (hdr *Header) Region() (*Tag, error) {
	if err := hdr.setRegion(new(rpmHeaderPre)); err != nil {
		return nil, err
	}
	return hdr.region, nil
}

func (hdr *Header) writeRegionHeader(w io.Writer) error {
	if hdr.region == nil {
		return nil
	}
	return hdr.region.writeHeader(w)
}

func (hdr *Header) writeRegionData(w io.Writer) (int64, error) {
	if hdr.region == nil {
		return 0, nil
	}
	return hdr.region.data.WriteTo(w)
}

func (hdr *Header) MarshalJSON() ([]byte, error) {
	if err := hdr.setRegion(new(rpmHeaderPre)); err != nil {
		return nil, err
	}
	if hdr.region != nil {
		return json.Marshal(append([]*Tag{hdr.region}, hdr.Tags...))
	}
	return json.Marshal(hdr.Tags)
}

func (hdr *Header) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &hdr.Tags); err != nil {
		return err
	}
	if len(hdr.Tags) == 0 {
		return nil
	}

	// region tag should be the last tag in offset order
	sort.Sort(hdr)

	lt := hdr.Tags[len(hdr.Tags)-1]
	switch lt.Tag {
	case HEADER_IMMUTABLE, HEADER_SIGNATURES:
		hdr.SetRegion(lt.Tag)
		hdr.Tags = hdr.Tags[:len(hdr.Tags)-1]
		hdr.off = lt.Offset
	default:
		hdr.off = lt.Offset + uint32(lt.data.Len())
	}
	return nil
}

func (hdr *Header) align(n uint32) uint32 {
	return (hdr.off + n) &^ n
}

// TODO: this
var errHeaderOverflow = errors.New("rpm: header offset overflow")

func (hdr *Header) Add(tag *Tag) error {
	switch tag.Type {
	case RPM_INT16_TYPE:
		hdr.off = hdr.align(0x1)
	case RPM_INT32_TYPE:
		hdr.off = hdr.align(0x3)
	case RPM_INT64_TYPE:
		hdr.off = hdr.align(0x7)
	}
	tag.Offset = hdr.off
	hdr.off += uint32(tag.data.Len())
	hdr.Tags = append(hdr.Tags, tag)
	return nil
}

const zs = 8

var zb [zs]byte

var errInvalidOffset = errors.New("rpm: invalid tag offset")

func (hdr *Header) pad(w io.Writer, off uint32, cur int64) (int, error) {
	if int64(off) < cur {
		return 0, errInvalidOffset
	}
	if n := int64(off) - cur; n > zs {
		return 0, errInvalidOffset
	} else {
		return w.Write(zb[:n])
	}
}

var (
	errNoTags  = errors.New("rpm: no tags")
	errDataLen = errors.New("rpm: data length mismatch")
)

func (hdr *Header) WriteTo(w io.Writer) (int64, error) {
	if len(hdr.Tags) == 0 {
		return 0, errNoTags
	}

	pre := &rpmHeaderPre{
		Magic:  rpmHeaderMagic,
		Count:  uint32(len(hdr.Tags)),
		Length: hdr.off,
	}
	if err := hdr.setRegion(pre); err != nil {
		return 0, err
	}
	if err := binary.Write(w, binary.BigEndian, pre); err != nil {
		return 0, err
	}

	// "region tag" needs to get written out first
	if err := hdr.writeRegionHeader(w); err != nil {
		return 0, err
	}

	// write out tags and data in offset order
	sort.Sort(hdr)

	for _, v := range hdr.Tags {
		if err := v.writeHeader(w); err != nil {
			return 0, err
		}
	}

	var cur int64
	for _, v := range hdr.Tags {
		n1, err := hdr.pad(w, v.Offset, cur)
		if err != nil {
			return 0, err
		}

		n2, err := v.data.WriteTo(w)
		if err != nil {
			return 0, err
		}

		cur += int64(n1) + n2
	}

	n, err := hdr.writeRegionData(w)
	if err != nil {
		return 0, err
	}

	if n+cur != int64(pre.Length) {
		return 0, errDataLen
	}

	r := pre.Count * tagSize
	r += pre.Length
	r += 16 // len(pre)
	return int64(r), nil
}

func WriteHeaders(w io.Writer, hdr ...io.WriterTo) (int64, error) {
	var r int64
	for _, v := range hdr {
		// headers need to be 8b aligned
		p := (r + 0x7) &^ 0x7
		a, err := w.Write(zb[:p-r])
		if err != nil {
			return 0, err
		}
		r += int64(a)

		n, err := v.WriteTo(w)
		if err != nil {
			return 0, err
		}
		r += n
	}
	return r, nil
}
