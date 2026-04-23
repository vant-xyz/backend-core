package utils

import (
	"strings"
	"testing"
)

const alphanumericSet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const numericSet = "0123456789"

func TestRandomAlphanumeric_Length(t *testing.T) {
	for _, n := range []int{1, 6, 12, 32, 64} {
		got := RandomAlphanumeric(n)
		if len(got) != n {
			t.Errorf("RandomAlphanumeric(%d) returned length %d", n, len(got))
		}
	}
}

func TestRandomAlphanumeric_OnlyAlphanumericChars(t *testing.T) {
	s := RandomAlphanumeric(200)
	for _, c := range s {
		if !strings.ContainsRune(alphanumericSet, c) {
			t.Errorf("RandomAlphanumeric produced non-alphanumeric char %q", c)
		}
	}
}

func TestRandomAlphanumeric_TwoCallsDiffer(t *testing.T) {
	a, b := RandomAlphanumeric(16), RandomAlphanumeric(16)
	if a == b {
		t.Errorf("two RandomAlphanumeric(16) calls produced identical output %q", a)
	}
}

func TestRandomNumbers_Length(t *testing.T) {
	for _, n := range []int{1, 4, 8, 16} {
		got := RandomNumbers(n)
		if len(got) != n {
			t.Errorf("RandomNumbers(%d) returned length %d", n, len(got))
		}
	}
}

func TestRandomNumbers_OnlyDigits(t *testing.T) {
	s := RandomNumbers(200)
	for _, c := range s {
		if !strings.ContainsRune(numericSet, c) {
			t.Errorf("RandomNumbers produced non-digit char %q", c)
		}
	}
}

func TestRandomNumbers_TwoCallsDiffer(t *testing.T) {
	a, b := RandomNumbers(12), RandomNumbers(12)
	if a == b {
		t.Errorf("two RandomNumbers(12) calls produced identical output %q", a)
	}
}
