package base58

import "testing"

func TestDecodeKnownValue(t *testing.T) {
	decoded, err := Decode("1112")
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 4 || decoded[0] != 0 || decoded[1] != 0 || decoded[2] != 0 || decoded[3] != 1 {
		t.Fatalf("unexpected bytes: %v", decoded)
	}
}

func TestDecodeRejectsInvalidCharacter(t *testing.T) {
	if _, err := Decode("0OIl"); err == nil {
		t.Fatal("expected invalid base58 error")
	}
}
