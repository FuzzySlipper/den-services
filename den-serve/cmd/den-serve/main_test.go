package main

import "testing"

func TestNewManagerUsesBuiltInDefaultsWhenConfigPathIsEmpty(t *testing.T) {
	if _, err := newManager(""); err != nil {
		t.Fatalf("newManager(\"\") error = %v", err)
	}
}
