package models

import (
	"database/sql"
	"testing"
)

func TestEncodeMemoryTags(t *testing.T) {
	ns, err := EncodeMemoryTags(nil)
	if err != nil || ns.Valid {
		t.Fatalf("nil: %#v %v", ns, err)
	}
	ns, err = EncodeMemoryTags([]string{"  ", ""})
	if err != nil || ns.Valid {
		t.Fatalf("empty trim: %#v %v", ns, err)
	}
	ns, err = EncodeMemoryTags([]string{" a ", "b"})
	if err != nil || !ns.Valid {
		t.Fatalf("tags: %#v %v", ns, err)
	}
	out, err := DecodeMemoryTags(ns)
	if err != nil || len(out) != 2 || out[0] != "a" {
		t.Fatalf("decode: %#v %v", out, err)
	}
}

func TestDecodeMemoryTagsInvalid(t *testing.T) {
	_, err := DecodeMemoryTags(sql.NullString{String: "not-json", Valid: true})
	if err == nil {
		t.Fatal("expected error")
	}
	_, err = DecodeMemoryTags(sql.NullString{})
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
}
