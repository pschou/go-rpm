package scpio

import (
	"errors"
	"io"
)

const (
	newcMagic  = "070701"
	scpioMagic = "07070X"
)

func fmt16(n uint32, r []byte) {
	const digits = "0123456789abcdef"
	for i := 0; i < 8; i++ {
		r[7-i] = digits[n>>(i*4)&0xf]
	}
}

func newHeader(magic string, u ...uint32) []byte {
	r := make([]byte, len(u)*8+6)
	copy(r[:6], magic)
	for i := range u {
		fmt16(u[i], r[i*8+6:])
	}
	return r
}

type Writer struct {
	off int
	w   io.Writer
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

var errShortWrite = errors.New("scpio: short write")

func (s *Writer) Write(b []byte) (int, error) {
	n, err := s.w.Write(b)
	if err != nil {
		return 0, err
	}
	if n != len(b) {
		return 0, errShortWrite
	}
	s.off += n
	return n, err
}

var zb [4]byte

func (s *Writer) align() error {
	n := (s.off + 0x3) &^ 0x3
	_, err := s.Write(zb[:n-s.off])
	return err
}

var trailer []byte = append(
	newHeader(newcMagic, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 11, 0),
	"TRAILER!!!\x00"...,
)

func (s *Writer) writeHeader(ino uint32, close bool) (err error) {
	if err = s.align(); err != nil {
		return err
	}
	if close {
		_, err = s.Write(trailer)
	} else {
		_, err = s.Write(newHeader(scpioMagic, ino))
	}
	if err != nil {
		return err
	}
	return s.align()
}

func (s *Writer) WriteHeader(ino uint32) error {
	return s.writeHeader(ino, false)
}

func (s *Writer) Close() error {
	return s.writeHeader(0, true)
}
