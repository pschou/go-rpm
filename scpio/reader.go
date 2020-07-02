package scpio

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
)

type Reader struct {
	r   io.Reader
	off int
}

func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

var (
	errUnexpectedEOF  = errors.New("scpio: unexpected EOF")
	errBadMagic       = errors.New("scpio: bad magic")
	errInvalidTrailer = errors.New("scpio: invalid trailer")
)

func (r *Reader) align() error {
	i := (r.off + 0x3) &^ 0x3
	lr := &io.LimitedReader{r.r, int64(i - r.off)}
	n, err := io.Copy(ioutil.Discard, lr)
	if err != nil {
		return err
	}
	if lr.N != 0 {
		return errUnexpectedEOF
	}
	r.off += int(n)
	return nil
}

func (r *Reader) trailer() error {
	const (
		trailer = "TRAILER!!!\x00"
		tl      = 8*12 + len(trailer)
	)
	b := make([]byte, tl-2)
	n, err := io.ReadFull(r.r, b)
	if err != nil {
		return err
	}
	if string(b[n-len(trailer):]) != trailer {
		return errInvalidTrailer
	}
	r.off += n
	return r.align()
}

func (r *Reader) err(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("offset: 0x%x, %v", r.off, err)
}

func (r *Reader) Next(sz int) (uint32, error) {
	r.off += sz
	if err := r.align(); err != nil {
		return 0, r.err(err)
	}

	b := make([]byte, 6+8+2)
	n, err := io.ReadFull(r.r, b)
	if err != nil {
		return 0, r.err(err)
	}
	r.off += n

	switch string(b[:6]) {
	case scpioMagic:
	case newcMagic:
		return 0, r.err(r.trailer())
	default:
		return 0, r.err(errBadMagic)
	}

	var d [4]byte
	if _, err := hex.Decode(d[:], b[6:14]); err != nil {
		return 0, r.err(err)
	}

	return binary.BigEndian.Uint32(d[:]), nil
}
