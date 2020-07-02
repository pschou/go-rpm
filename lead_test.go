package rpm

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLeadRW(t *testing.T) {
	la := NewLead("lead", LeadBinary)
	b := new(bytes.Buffer)
	if _, err := la.WriteTo(b); err != nil {
		t.Fatalf("lead write: %v", err)
	}

	lb, err := NewReader(b).Lead()
	if err != nil {
		t.Fatalf("lead read: %v", err)
	}

	if *la != *lb {
		t.Fatalf("la != lb")
	}
}

func TestLeadMagic(t *testing.T) {
	la := NewLead("lead", LeadBinary)
	copy(la.Magic[:], "test")
	b := new(bytes.Buffer)
	if _, err := la.WriteTo(b); err != nil {
		t.Fatalf("lead write: %v", err)
	}

	_, err := NewReader(b).Lead()
	if err != errInvalidLead {
		t.Fatalf("expected invalid lead: got %v", err)
	}
}

func TestLeadJSON(t *testing.T) {
	la := NewLead("lead", LeadBinary)
	b, err := json.Marshal(la)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}

	var lb Lead
	if err := json.Unmarshal(b, &lb); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if *la != lb {
		t.Fatalf("la != lb")
	}
}
