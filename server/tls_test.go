package server

import (
	"crypto/tls"
	"errors"
	"testing"
)

func TestTLSConfig(t *testing.T) {
	t.Run("answers nil when no material is set", func(t *testing.T) {
		cfg, err := TLSConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg != nil {
			t.Fatalf("config = %+v, want nil for cleartext", cfg)
		}
	})

	t.Run("loads the certificate and key", func(t *testing.T) {
		certPEM, keyPEM := selfSignedPEM(t)
		t.Setenv(EnvTLSCert, certPEM)
		t.Setenv(EnvTLSKey, keyPEM)

		cfg, err := TLSConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.Certificates) != 1 {
			t.Fatalf("certificates = %d, want the one loaded", len(cfg.Certificates))
		}
		if cfg.ClientAuth != tls.NoClientCert {
			t.Errorf("client auth = %v, want none asked for without a CA", cfg.ClientAuth)
		}
		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("min version = %d, want TLS 1.2", cfg.MinVersion)
		}
	})

	t.Run("requires a client certificate when a CA is set", func(t *testing.T) {
		certPEM, keyPEM := selfSignedPEM(t)
		t.Setenv(EnvTLSCert, certPEM)
		t.Setenv(EnvTLSKey, keyPEM)
		// Any certificate serves as the CA: the pool holds it, the handshake
		// is where it would be checked.
		t.Setenv(EnvTLSClientCA, certPEM)

		cfg, err := TLSConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
			t.Errorf("client auth = %v, want required and verified", cfg.ClientAuth)
		}
		if cfg.ClientCAs == nil {
			t.Error("no client CA pool")
		}
	})

	t.Run("fails on a key without a certificate", func(t *testing.T) {
		_, keyPEM := selfSignedPEM(t)
		t.Setenv(EnvTLSKey, keyPEM)

		if _, err := TLSConfig(); err == nil {
			t.Fatal("expected the missing certificate to be reported")
		}
	})

	t.Run("fails on a CA that holds no certificate", func(t *testing.T) {
		certPEM, keyPEM := selfSignedPEM(t)
		t.Setenv(EnvTLSCert, certPEM)
		t.Setenv(EnvTLSKey, keyPEM)
		t.Setenv(EnvTLSClientCA, "not pem")

		_, err := TLSConfig()
		if !errors.Is(err, ErrTLSClientCA) {
			t.Fatalf("error = %v, want %v", err, ErrTLSClientCA)
		}
	})
}
