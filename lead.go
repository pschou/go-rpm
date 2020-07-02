package rpm

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
)

var leadMagic = [...]byte{0xed, 0xab, 0xee, 0xdb}

type LeadType uint16

const (
	LeadBinary LeadType = iota
	LeadSource
)

type leadName [66]byte

func (l leadName) MarshalJSON() ([]byte, error) {
	var i int
	if i = bytes.IndexByte(l[:], 0); i == -1 {
		i = len(l)
	}
	return json.Marshal(string(l[:i]))
}

func (l *leadName) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	copy((*l)[:], name)
	return nil
}

type Lead struct {
	Magic         [4]byte
	Major         uint8
	Minor         uint8
	Type          LeadType
	ArchNum       uint16
	Name          leadName
	OsNum         uint16
	SignatureType uint16
	// pad to 96 bytes, 8 byte aligned
	_ [16]byte
}

func NewLead(name string, lt LeadType) *Lead {
	// defined as 5 in lib/rpmlead.c, 3.0 signature type
	const headerSigType = 5

	r := &Lead{
		Magic:         leadMagic,
		Major:         3,
		Minor:         0,
		SignatureType: headerSigType,
		Type:          lt,
		ArchNum:       1, // i386/x86_64
		OsNum:         1, // linux
	}

	// defined again in the payload header
	copy(r.Name[:], name)
	return r
}

func (l *Lead) WriteTo(w io.Writer) (int64, error) {
	return 96, binary.Write(w, binary.BigEndian, l)
}
