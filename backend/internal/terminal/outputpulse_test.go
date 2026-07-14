package terminal

import (
	"testing"
	"time"
)

func TestOutputPulseTouchAndLastOutputAt(t *testing.T) {
	p := NewOutputPulse()
	if _, ok := p.LastOutputAt("h1"); ok {
		t.Fatal("LastOutputAt on an untouched id must report ok=false")
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p.Touch("h1", now)
	got, ok := p.LastOutputAt("h1")
	if !ok || !got.Equal(now) {
		t.Fatalf("LastOutputAt(h1) = %v, %v; want %v, true", got, ok, now)
	}
	// A later touch overwrites, it does not accumulate.
	later := now.Add(time.Minute)
	p.Touch("h1", later)
	got, ok = p.LastOutputAt("h1")
	if !ok || !got.Equal(later) {
		t.Fatalf("LastOutputAt(h1) after second touch = %v, %v; want %v, true", got, ok, later)
	}
}

func TestOutputPulseNilSafe(t *testing.T) {
	var p *OutputPulse
	// A nil *OutputPulse must behave like an always-empty registry: every
	// attachment defaults to pulse == nil, and copyOut calls Touch
	// unconditionally, so both methods must tolerate a nil receiver.
	p.Touch("h1", time.Now())
	if _, ok := p.LastOutputAt("h1"); ok {
		t.Fatal("nil *OutputPulse must report ok=false, never panic or record")
	}
}

func TestOutputPulseEmptyIDIgnored(t *testing.T) {
	p := NewOutputPulse()
	p.Touch("", time.Now())
	if _, ok := p.LastOutputAt(""); ok {
		t.Fatal("empty id must never be recorded or reported present")
	}
}
