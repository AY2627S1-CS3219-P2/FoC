// AI Assistance Disclosure:
// Tool: Codex (GPT-5), date: 2026-09-20
// Scope: Added the recorded PEM key-set loader and SHA-256 key-ID derivation.
// Author review: COMPLETED BY ZI YANG

package auth

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

type keySetFile struct {
	Keys []string `json:"keys"`
}

// LoadKeySet reads the recorded JSON key-set file. The first private key is
// active for signing; remaining keys remain available for verification/JWKS.
func LoadKeySet(path string) (KeySet, error) {
	if path == "" {
		return KeySet{}, errors.New("JWT key-set path is required")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return KeySet{}, fmt.Errorf("read JWT key-set file: %w", err)
	}
	var file keySetFile
	if err := json.Unmarshal(contents, &file); err != nil {
		return KeySet{}, fmt.Errorf("decode JWT key-set file: %w", err)
	}
	if len(file.Keys) == 0 {
		return KeySet{}, errors.New("JWT key-set file has no keys")
	}

	keys := make([]Key, 0, len(file.Keys))
	for index, encoded := range file.Keys {
		privateKey, err := parseRSAPrivateKey([]byte(encoded))
		if err != nil {
			return KeySet{}, fmt.Errorf("read JWT key at index %d: %w", index, err)
		}
		key := Key{ID: keyID(&privateKey.PublicKey), PublicKey: &privateKey.PublicKey}
		if index == 0 {
			key.PrivateKey = privateKey
		}
		keys = append(keys, key)
	}
	return NewKeySet(keys, keys[0].ID)
}

func parseRSAPrivateKey(encoded []byte) (*rsa.PrivateKey, error) {
	block, remainder := pem.Decode(encoded)
	if block == nil || len(remainder) != 0 {
		return nil, errors.New("private key must contain exactly one PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if privateKey, pkcs1Err := x509.ParsePKCS1PrivateKey(block.Bytes); pkcs1Err == nil {
			return privateKey, nil
		}
		return nil, errors.New("private key is not a supported RSA key")
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return privateKey, nil
}

func keyID(publicKey *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		panic(err)
	}
	fingerprint := sha256.Sum256(der)
	return hex.EncodeToString(fingerprint[:])
}
