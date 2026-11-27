package supermarket

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustPEM(t *testing.T, block *pem.Block) []byte {
	t.Helper()
	return pem.EncodeToMemory(block)
}

func TestParseKeyAcceptsPKCS1(t *testing.T) {
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	data := mustPEM(t, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rk)})
	got, err := ParseKey(data)
	if err != nil {
		t.Fatalf("ParseKey PKCS#1: %v", err)
	}
	if got.N.Cmp(rk.N) != 0 {
		t.Error("parsed PKCS#1 key does not match the input")
	}
}

func TestParseKeyAcceptsPKCS8(t *testing.T) {
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(rk)
	if err != nil {
		t.Fatal(err)
	}
	data := mustPEM(t, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
	got, err := ParseKey(data)
	if err != nil {
		t.Fatalf("ParseKey PKCS#8: %v", err)
	}
	if got.N.Cmp(rk.N) != 0 {
		t.Error("parsed PKCS#8 key does not match the input")
	}
}

func TestParseKeyRejectsNonRSAPKCS8(t *testing.T) {
	ek, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ek)
	if err != nil {
		t.Fatal(err)
	}
	data := mustPEM(t, &pem.Block{Type: "PRIVATE KEY", Bytes: der})
	_, err = ParseKey(data)
	if err == nil || !strings.Contains(err.Error(), "*rsa.PrivateKey") {
		t.Errorf("err = %v, want a *rsa.PrivateKey-mismatch error", err)
	}
}

func TestParseKeyRejectsGarbage(t *testing.T) {
	if _, err := ParseKey([]byte("not a key")); err == nil {
		t.Error("expected an error for non-PEM garbage")
	}
	der := []byte("not-actually-der")
	if _, err := ParseKey(mustPEM(t, &pem.Block{Type: "PRIVATE KEY", Bytes: der})); err == nil {
		t.Error("expected an error for a PEM block wrapping garbage DER")
	}
}

func TestLoadKeyFileMissingFile(t *testing.T) {
	_, err := LoadKeyFile(filepath.Join(t.TempDir(), "nope.pem"))
	if err == nil {
		t.Error("expected an error from LoadKeyFile on a missing path")
	}
}

func TestLoadKeyFileRoundTrip(t *testing.T) {
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key.pem")
	data := mustPEM(t, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rk)})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadKeyFile(path)
	if err != nil {
		t.Fatalf("LoadKeyFile: %v", err)
	}
	if loaded.N.Cmp(rk.N) != 0 {
		t.Error("loaded key does not match the one on disk")
	}
}

func TestParseKeyInvalidContentMatchesErrInvalidKey(t *testing.T) {
	ek, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecDER, err := x509.MarshalPKCS8PrivateKey(ek)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"empty":          nil,
		"not PEM":        []byte("not a key"),
		"garbage DER":    mustPEM(t, &pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not-actually-der")}),
		"non-RSA PKCS#8": mustPEM(t, &pem.Block{Type: "PRIVATE KEY", Bytes: ecDER}),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseKey(data)
			if !errors.Is(err, ErrInvalidKey) {
				t.Errorf("err = %v, want errors.Is(err, ErrInvalidKey); callers can't tell a bad key from other failures", err)
			}
		})
	}
}

func TestLoadKeyFileSeparatesFileErrorsFromBadKeys(t *testing.T) {
	_, err := LoadKeyFile(filepath.Join(t.TempDir(), "nope.pem"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: err = %v, want errors.Is(err, fs.ErrNotExist)", err)
	}
	if errors.Is(err, ErrInvalidKey) {
		t.Errorf("missing file: err = %v matches ErrInvalidKey; a caller would blame the key's content", err)
	}

	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = LoadKeyFile(path)
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("bad content: err = %v, want errors.Is(err, ErrInvalidKey)", err)
	}
}
