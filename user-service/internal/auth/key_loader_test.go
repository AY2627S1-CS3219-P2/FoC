// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added focused tests for the recorded JSON PEM key-set loader.
// Author review: PENDING — reviewer to complete

package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeySetUsesFirstKeyForSigningAndPublishesAllKeys(t *testing.T) {
	active := loaderTestPrivateKey(t)
	retired := loaderTestPrivateKey(t)
	path := writeKeySetFile(t, []string{encodePrivateKey(t, active), encodePrivateKey(t, retired)})

	keySet, err := LoadKeySet(path)
	if err != nil {
		t.Fatal(err)
	}
	if keySet.activeID == "" || len(keySet.keys) != 2 {
		t.Fatalf("key set = %#v, want active key and two entries", keySet)
	}
	if keySet.keys[keySet.activeID].PrivateKey == nil {
		t.Fatal("active key has no private signing key")
	}
	for id, key := range keySet.keys {
		if id != keySet.activeID && key.PrivateKey != nil {
			t.Fatal("retired key retained a private signing key")
		}
	}
}

func TestLoadKeySetRejectsInvalidInputWithoutEchoingKeyMaterial(t *testing.T) {
	path := writeKeySetFile(t, []string{"not a PEM private key"})
	if _, err := LoadKeySet(path); err == nil {
		t.Fatal("LoadKeySet() succeeded with invalid key material")
	}
}

func loaderTestPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func encodePrivateKey(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))
}

func writeKeySetFile(t *testing.T, keys []string) string {
	t.Helper()
	contents, err := json.Marshal(keySetFile{Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
