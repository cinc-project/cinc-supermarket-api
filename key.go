package supermarket

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// ParseKey parses a PEM-encoded RSA private key (PKCS#1 or PKCS#8). Data
// that is not such a key returns an error matching ErrInvalidKey.
func ParseKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("%w: no PEM block found in key data", ErrInvalidKey)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse private key: %w", ErrInvalidKey, err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: key is %T, want *rsa.PrivateKey", ErrInvalidKey, parsed)
	}
	return key, nil
}

// LoadKeyFile reads and parses an RSA private key from a file path. A file
// that can't be read returns an error wrapping the fs error (for example
// fs.ErrNotExist or fs.ErrPermission); content that is not a usable key
// returns an error matching ErrInvalidKey, as from ParseKey.
func LoadKeyFile(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("supermarket: read key file: %w", err)
	}
	return ParseKey(data)
}

// ErrInvalidKey matches a ParseKey or LoadKeyFile error for key data that is
// not a usable PEM-encoded RSA private key.
var ErrInvalidKey = errors.New("supermarket: invalid private key")
