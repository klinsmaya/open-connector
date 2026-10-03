package credentials

import (
	"bytes"
	"testing"
)

func TestVaultBindsCiphertextToProjectAndRecord(t *testing.T) {
	vault, err := NewVault(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("fixture-upstream-runtime-secret")
	sealed, err := vault.Seal(secret, "project-a/session-a")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, secret) {
		t.Fatal("plaintext retained")
	}
	if _, err = vault.Open(sealed, "project-b/session-a"); err == nil {
		t.Fatal("cross-project ciphertext accepted")
	}
	out, err := vault.Open(sealed, "project-a/session-a")
	if err != nil || !bytes.Equal(out, secret) {
		t.Fatal("round trip failed")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err = vault.Open(sealed, "project-a/session-a"); err == nil {
		t.Fatal("tampered credential accepted")
	}
}
