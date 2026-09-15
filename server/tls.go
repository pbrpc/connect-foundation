package server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// Environment variable names for the listener's TLS material. Each holds PEM,
// the material itself rather than a path to it, so a process with no
// filesystem is configured the same way as one with.
const (
	EnvTLSCert     = "TLS_CERT"
	EnvTLSKey      = "TLS_KEY"
	EnvTLSClientCA = "TLS_CLIENT_CA"
)

// ErrTLSClientCA reports a TLS_CLIENT_CA that holds no certificate.
var ErrTLSClientCA = errors.New("no certificate found")

// TLSConfig reads the listener's TLS material from the environment: the
// server certificate and key from TLS_CERT and TLS_KEY, and, when
// TLS_CLIENT_CA holds a CA, a requirement that every client present a
// certificate that CA signed. With TLS_CERT and TLS_KEY both unset it answers
// nil, which WithTLS reads as cleartext. One of them without the other, or PEM
// that does not parse, is an error.
func TLSConfig() (*tls.Config, error) {
	certPEM, keyPEM := os.Getenv(EnvTLSCert), os.Getenv(EnvTLSKey)
	if certPEM == "" && keyPEM == "" {
		return nil, nil //nolint:nilnil // no material is cleartext, not a failure
	}

	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("%s and %s: %w", EnvTLSCert, EnvTLSKey, err)
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if caPEM := os.Getenv(EnvTLSClientCA); caPEM != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, fmt.Errorf("%s: %w", EnvTLSClientCA, ErrTLSClientCA)
		}

		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return cfg, nil
}
