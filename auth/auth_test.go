package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"math/big"
	"os"
	"strings"
	"testing"
)

func TestParsePrivateKeySupportsPKCS1AndPKCS8(t *testing.T) {
	key := generateTestRSAKey(t)

	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes})

	cred1, err := ParsePrivateKey(pkcs1)
	if err != nil {
		t.Fatalf("ParsePrivateKey(PKCS#1): %v", err)
	}
	cred2, err := ParsePrivateKey(pkcs8)
	if err != nil {
		t.Fatalf("ParsePrivateKey(PKCS#8): %v", err)
	}

	if cred1.RSAPrivateKey().N.Cmp(key.N) != 0 {
		t.Fatalf("PKCS#1 credential has different modulus")
	}
	if cred2.RSAPrivateKey().N.Cmp(key.N) != 0 {
		t.Fatalf("PKCS#8 credential has different modulus")
	}
	if !bytes.Equal(cred1.PublicKeyPayload(), cred2.PublicKeyPayload()) {
		t.Fatalf("PKCS#1 and PKCS#8 public key payloads differ")
	}
}

func TestLoadPrivateKeyReadsExplicitPath(t *testing.T) {
	key := generateTestRSAKey(t)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	path := writeTempFile(t, pemBytes)
	cred, err := LoadPrivateKey(path)
	if err != nil {
		t.Fatalf("LoadPrivateKey: %v", err)
	}
	if cred.Signer() == nil {
		t.Fatalf("Signer() is nil")
	}
}

func TestPublicKeyPayloadIsDeterministicNULTerminatedADBPublicKey(t *testing.T) {
	key := generateTestRSAKey(t)
	cred, err := NewCredentialWithComment(key, "test@host")
	if err != nil {
		t.Fatalf("NewCredentialWithComment: %v", err)
	}

	payload1 := cred.PublicKeyPayload()
	payload2 := cred.PublicKeyPayload()
	if !bytes.Equal(payload1, payload2) {
		t.Fatalf("PublicKeyPayload is not deterministic")
	}
	if payload1[len(payload1)-1] != 0 {
		t.Fatalf("PublicKeyPayload is not NUL terminated: %q", payload1)
	}

	text := strings.TrimSuffix(string(payload1), "\x00")
	parts := strings.SplitN(text, " ", 2)
	if len(parts) != 2 || parts[1] != "test@host" {
		t.Fatalf("payload comment mismatch: %q", text)
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode public key base64: %v", err)
	}
	if len(decoded) != 4+4+androidRSAModulusSizeBytes+androidRSAModulusSizeBytes+4 {
		t.Fatalf("decoded ADB public key length = %d", len(decoded))
	}
	if got := binary.LittleEndian.Uint32(decoded[0:4]); got != androidRSAModulusSizeWords {
		t.Fatalf("modulus words = %d", got)
	}
	if got := littleEndianInteger(decoded[8 : 8+androidRSAModulusSizeBytes]); got.Cmp(key.N) != 0 {
		t.Fatalf("decoded modulus differs from RSA key")
	}
	exponentOffset := 8 + androidRSAModulusSizeBytes + androidRSAModulusSizeBytes
	if got := binary.LittleEndian.Uint32(decoded[exponentOffset : exponentOffset+4]); got != uint32(key.E) {
		t.Fatalf("exponent = %d, want %d", got, key.E)
	}

	payload1[0] ^= 0xff
	if bytes.Equal(payload1, cred.PublicKeyPayload()) {
		t.Fatalf("PublicKeyPayload did not return a defensive copy")
	}
}

func TestParsePrivateKeyRejectsUnsupportedMalformedAndEncryptedKeys(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "not PEM", data: []byte("not a key"), want: "not PEM"},
		{name: "encrypted PEM", data: pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte{1, 2, 3}}), want: "encrypted"},
		{name: "unsupported PEM type", data: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte{1, 2, 3}}), want: "unsupported"},
		{name: "malformed RSA", data: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte{1, 2, 3}}), want: "PKCS#1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePrivateKey(tt.data)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParsePrivateKey error = %v, want substring %q", err, tt.want)
			}
		})
	}

	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(ECDSA): %v", err)
	}
	ecdsaBytes, err := x509.MarshalPKCS8PrivateKey(ecdsaKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey(ECDSA): %v", err)
	}
	_, err = ParsePrivateKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecdsaBytes}))
	if err == nil || !strings.Contains(err.Error(), "not *rsa.PrivateKey") {
		t.Fatalf("ParsePrivateKey(ECDSA) error = %v", err)
	}
}

func TestNewCredentialRejectsInvalidInputs(t *testing.T) {
	if _, err := NewCredential(nil); err == nil {
		t.Fatalf("NewCredential(nil) succeeded")
	}

	smallKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey(1024): %v", err)
	}
	if _, err := NewCredential(smallKey); err == nil || !strings.Contains(err.Error(), "2048 bits") {
		t.Fatalf("NewCredential(1024-bit) error = %v", err)
	}

	key := generateTestRSAKey(t)
	if _, err := NewCredentialWithComment(key, "bad\ncomment"); err == nil || !strings.Contains(err.Error(), "control") {
		t.Fatalf("NewCredentialWithComment(bad comment) error = %v", err)
	}
}

func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "adbkey-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return file.Name()
}

func littleEndianInteger(data []byte) *big.Int {
	reversed := make([]byte, len(data))
	for i := range data {
		reversed[len(data)-1-i] = data[i]
	}
	return new(big.Int).SetBytes(reversed)
}
