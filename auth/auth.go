// Package auth provides explicit ADB host authentication credentials.
package auth

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"strings"
)

const (
	androidRSAModulusSizeWords = 64
	androidRSAModulusSizeBytes = androidRSAModulusSizeWords * 4
	defaultPublicKeyComment    = "adb-go"
)

// Credential is an explicit ADB host credential loaded or constructed by the
// caller. It contains an RSA private key for future AUTH TOKEN signing and the
// corresponding AUTH RSAPUBLICKEY payload.
type Credential struct {
	privateKey       *rsa.PrivateKey
	publicKeyPayload []byte
}

// LoadPrivateKey loads an unencrypted RSA private key from path and returns an
// ADB host credential. The path is explicit by design; adb-go does not discover,
// create, persist, or rotate key files.
func LoadPrivateKey(path string) (*Credential, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("adb auth private key path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read adb auth private key %q: %w", path, err)
	}

	cred, err := ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse adb auth private key %q: %w", path, err)
	}
	return cred, nil
}

// ParsePrivateKey parses an unencrypted PEM-encoded RSA private key and returns
// an ADB host credential. PKCS#1 "RSA PRIVATE KEY" and PKCS#8 "PRIVATE KEY"
// blocks are supported because those cover common adbkey files.
func ParsePrivateKey(data []byte) (*Credential, error) {
	block, _ := pem.Decode(bytes.TrimSpace(data))
	if block == nil {
		return nil, fmt.Errorf("adb auth private key is not PEM encoded")
	}
	if strings.Contains(block.Type, "ENCRYPTED") {
		return nil, fmt.Errorf("adb auth encrypted private keys are not supported")
	}

	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#1 RSA private key: %w", err)
		}
		key = parsed
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS#8 private key: %w", err)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("adb auth private key is %T, not *rsa.PrivateKey", parsed)
		}
		key = rsaKey
	default:
		return nil, fmt.Errorf("adb auth unsupported PEM block type %q", block.Type)
	}

	return NewCredential(key)
}

// NewCredential returns an ADB host credential for key using adb-go's default
// public-key comment.
func NewCredential(key *rsa.PrivateKey) (*Credential, error) {
	return NewCredentialWithComment(key, defaultPublicKeyComment)
}

// NewCredentialWithComment returns an ADB host credential for key and comment.
// The comment is the text after the base64 key in the AUTH RSAPUBLICKEY payload.
func NewCredentialWithComment(key *rsa.PrivateKey, comment string) (*Credential, error) {
	if key == nil {
		return nil, fmt.Errorf("adb auth RSA private key is nil")
	}
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("adb auth RSA private key is invalid: %w", err)
	}
	if key.N.BitLen() != androidRSAModulusSizeBytes*8 {
		return nil, fmt.Errorf("adb auth RSA key must be %d bits, got %d", androidRSAModulusSizeBytes*8, key.N.BitLen())
	}
	if key.E <= 0 {
		return nil, fmt.Errorf("adb auth RSA public exponent must be positive")
	}
	if strings.ContainsAny(comment, "\x00\r\n") {
		return nil, fmt.Errorf("adb auth public key comment contains an invalid control character")
	}
	comment = strings.TrimSpace(comment)
	if comment == "" {
		comment = defaultPublicKeyComment
	}

	payload, err := encodePublicKeyPayload(&key.PublicKey, comment)
	if err != nil {
		return nil, err
	}
	return &Credential{privateKey: key, publicKeyPayload: payload}, nil
}

// Signer returns the credential's private key as a crypto.Signer for future ADB
// AUTH TOKEN signature exchange.
func (c *Credential) Signer() crypto.Signer {
	if c == nil {
		return nil
	}
	return c.privateKey
}

// RSAPrivateKey returns the credential's RSA private key.
func (c *Credential) RSAPrivateKey() *rsa.PrivateKey {
	if c == nil {
		return nil
	}
	return c.privateKey
}

// PublicKeyPayload returns a copy of the NUL-terminated AUTH RSAPUBLICKEY
// payload corresponding to this credential.
func (c *Credential) PublicKeyPayload() []byte {
	if c == nil {
		return nil
	}
	return bytes.Clone(c.publicKeyPayload)
}

func encodePublicKeyPayload(key *rsa.PublicKey, comment string) ([]byte, error) {
	adbKey, err := encodeAndroidRSAPublicKey(key)
	if err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(adbKey)
	payload := make([]byte, 0, len(encoded)+1+len(comment)+1)
	payload = append(payload, encoded...)
	payload = append(payload, ' ')
	payload = append(payload, comment...)
	payload = append(payload, 0)
	return payload, nil
}

func encodeAndroidRSAPublicKey(key *rsa.PublicKey) ([]byte, error) {
	if key == nil || key.N == nil {
		return nil, fmt.Errorf("adb auth RSA public key is nil")
	}
	if key.N.BitLen() != androidRSAModulusSizeBytes*8 {
		return nil, fmt.Errorf("adb auth RSA key must be %d bits, got %d", androidRSAModulusSizeBytes*8, key.N.BitLen())
	}

	modulus := littleEndianFixed(key.N, androidRSAModulusSizeBytes)
	rr := montgomeryRR(key.N, androidRSAModulusSizeBytes)
	n0inv, err := n0inv(key.N)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint32(androidRSAModulusSizeWords))
	_ = binary.Write(&out, binary.LittleEndian, n0inv)
	out.Write(modulus)
	out.Write(rr)
	_ = binary.Write(&out, binary.LittleEndian, uint32(key.E))
	return out.Bytes(), nil
}

func littleEndianFixed(n *big.Int, size int) []byte {
	out := make([]byte, size)
	be := n.Bytes()
	for i := 0; i < len(be) && i < size; i++ {
		out[i] = be[len(be)-1-i]
	}
	return out
}

func montgomeryRR(n *big.Int, modulusSizeBytes int) []byte {
	rr := new(big.Int).Lsh(big.NewInt(1), uint(modulusSizeBytes*8*2))
	rr.Mod(rr, n)
	return littleEndianFixed(rr, modulusSizeBytes)
}

func n0inv(n *big.Int) (uint32, error) {
	mod32 := new(big.Int).Lsh(big.NewInt(1), 32)
	n0 := new(big.Int).Mod(n, mod32)
	inv := new(big.Int).ModInverse(n0, mod32)
	if inv == nil {
		return 0, fmt.Errorf("adb auth RSA modulus is not invertible modulo 2^32")
	}
	inv32 := uint32(inv.Uint64())
	return 0 - inv32, nil
}
