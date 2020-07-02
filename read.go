package rpm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"sort"
)

type Reader struct {
	r   io.Reader
	lr  *io.LimitedReader
	off int
}

func NewReader(r io.Reader) *Reader {
	return &Reader{
		r:  r,
		lr: &io.LimitedReader{R: r},
	}
}

type tagError struct {
	t   *Tag
	err error
}

func (t tagError) Error() string {
	return t.err.Error() + ", tag: " + t.t.String()
}

func (t tagError) Is(err error) bool {
	return t.err == err
}

var errInvalidLead = errors.New("rpm: invalid lead")

func (r *Reader) Lead() (*Lead, error) {
	l := new(Lead)
	if err := binary.Read(r.r, binary.BigEndian, l); err != nil {
		return nil, err
	}
	if l.Magic != leadMagic {
		return nil, errInvalidLead
	}

	const leadsz = 96
	r.off += leadsz
	return l, nil
}

var errBadAlign = errors.New("rpm: bad alignment")

func (r *Reader) align() error {
	i := (r.off + 0x7) &^ 0x7
	r.lr.N = int64(i - r.off)
	n, err := io.Copy(ioutil.Discard, r.lr)
	if err != nil {
		return err
	}
	if r.lr.N != 0 {
		return errBadAlign
	}
	r.off += int(n)
	return err
}

var errInvalidHeader = errors.New("rpm: invalid header")

func (r *Reader) header() (*Header, error) {
	hdr := new(Header)
	if err := binary.Read(
		r.r, binary.BigEndian, &hdr.rpmHeaderPre,
	); err != nil {
		return nil, err
	}
	if hdr.Magic != rpmHeaderMagic {
		return nil, errInvalidHeader
	}
	r.off += tagSize
	return hdr, nil
}

func (r *Reader) tags(hdr *Header) error {
	th := new(tagHeader)
	for i := 0; i < int(hdr.Count); i++ {
		if err := binary.Read(r.r, binary.BigEndian, th); err != nil {
			return err
		}
		t := &Tag{
			tagHeader: *th,
			idx:       i,
		}
		if t.Offset > hdr.Length {
			return r.err(tagError{t, errOffsetOOB})
		}
		hdr.Tags = append(hdr.Tags, t)
		r.off += tagSize
	}
	return nil
}

func (r *Reader) tagaligned(tag *Tag) bool {
	switch tag.Type {
	case RPM_INT16_TYPE:
		return r.off&0x1 == 0
	case RPM_INT32_TYPE:
		return r.off&0x3 == 0
	case RPM_INT64_TYPE:
		return r.off&0x7 == 0
	}
	return true
}

var (
	errUnexpectedEOF = errors.New("rpm: unexpected EOF")
	errOffsetOOB     = errors.New("rpm: offset out of bounds")
)

type offsetError struct {
	off int
	err error
}

func (e offsetError) Error() string {
	return fmt.Sprintf("offset: 0x%x, %v", e.off, e.err)
}

func (e offsetError) Unwrap() error {
	return e.err
}

func (r *Reader) err(err error) error {
	return offsetError{r.off, err}
}

func (r *Reader) Next() (*Header, error) {
	if err := r.align(); err != nil {
		return nil, err
	}

	hdr, err := r.header()
	if err != nil {
		return nil, r.err(err)
	}

	if err := r.tags(hdr); err != nil {
		return nil, err
	}
	if len(hdr.Tags) == 0 {
		return hdr, nil
	}

	// TODO: remove and read tag data in unsorted order
	sort.Sort(hdr)

	for i, v := range hdr.Tags {
		if !r.tagaligned(v) {
			return nil, r.err(tagError{v, errBadAlign})
		}

		var nt uint32
		if i == int(hdr.Count)-1 {
			nt = hdr.Length
		} else {
			nt = hdr.Tags[i+1].Offset
		}

		// TODO: allow for overlapping data
		if nt <= v.Offset {
			return nil, r.err(tagError{v, errOffsetOOB})
		}

		// TODO: skip padding
		nr := nt - v.Offset

		// TODO: make this configurable
		// const dataMax = 1 << 20
		// if nr > dataMax {
		// 	return errTagSize
		// }

		if err := v.make(v.Offset, nt); err != nil {
			return nil, r.err(tagError{v, err})
		}

		r.lr.N = int64(nr)
		w, err := v.data.ReadFrom(r.lr)
		if err != nil {
			return nil, r.err(tagError{v, err})
		}

		if r.lr.N != 0 {
			// padding should always be less than 8b
			if r.lr.N >= 8 {
				return nil, r.err(tagError{v, errUnexpectedEOF})
			}
			dn, err := io.Copy(ioutil.Discard, r.lr)
			if err != nil {
				return nil, r.err(tagError{v, err})
			}
			w += dn
		}

		if int64(nr) != w {
			return nil, r.err(tagError{v, errUnexpectedEOF})
		}

		v.off = r.off
		r.off += int(w)
	}

	lt := hdr.Tags[len(hdr.Tags)-1]
	switch lt.Tag {
	case HEADER_IMMUTABLE, HEADER_SIGNATURES:
		hdr.SetRegion(lt.Tag)
		hdr.Tags = hdr.Tags[:len(hdr.Tags)-1]
		hdr.off = lt.Offset
	default:
		hdr.off = hdr.Length
	}

	return hdr, nil
}
