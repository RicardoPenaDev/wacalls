// Command e2egencert generates a throwaway self-signed TLS certificate used
// exclusively by the Playwright E2E harness (client/tests/e2e) to run the
// local GLPI mock over HTTPS. It is test tooling, not part of the WACalls
// product surface: it is never imported by cmd/server or internal/*, and it
// must not be used for anything beyond local, ephemeral E2E runs.
//
// Usage: go run ./cmd/e2egencert <output-dir>
//
// Writes <output-dir>/cert.pem and <output-dir>/key.pem. The key is generated
// fresh in memory on every invocation and is never a real secret or
// production credential.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: e2egencert <output-dir>")
		os.Exit(2)
	}
	outDir := os.Args[1]
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		fail("create output dir", err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fail("generate key", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		fail("generate serial", err)
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "wacalls-e2e-glpi-mock",
			Organization: []string{"WACalls E2E Test Fixture (not for production use)"},
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		fail("create certificate", err)
	}

	certOut, err := os.Create(filepath.Join(outDir, "cert.pem"))
	if err != nil {
		fail("open cert.pem", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		fail("write cert.pem", err)
	}
	if err := certOut.Close(); err != nil {
		fail("close cert.pem", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		fail("marshal key", err)
	}
	keyOut, err := os.OpenFile(filepath.Join(outDir, "key.pem"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		fail("open key.pem", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		fail("write key.pem", err)
	}
	if err := keyOut.Close(); err != nil {
		fail("close key.pem", err)
	}
}

func fail(step string, err error) {
	fmt.Fprintf(os.Stderr, "e2egencert: %s: %v\n", step, err)
	os.Exit(1)
}
