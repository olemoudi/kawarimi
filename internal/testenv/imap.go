package testenv

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// IMAPServer is a scripted TLS IMAP4 mock for the "reply ALIVE by email" check-in
// channel. It speaks just enough IMAP for deadswitch.CheckIMAPForAlive (greeting,
// LOGIN, SELECT, SEARCH, LOGOUT) over real TLS: StartIMAP generates a self-signed
// certificate for 127.0.0.1 and points KAWARIMI_IMAP_CA at it, so the production
// client verifies the connection exactly as it would against a real server.
type IMAPServer struct {
	Host string
	Port int

	ln net.Listener

	mu          sync.Mutex
	aliveIDs    []string
	rejectLogin bool
	commands    []string
}

// StartIMAP starts the mock and installs its CA via KAWARIMI_IMAP_CA.
func StartIMAP(t testing.TB) *IMAPServer {
	t.Helper()

	cert, caPEM := selfSignedCert(t)
	caPath := filepath.Join(t.TempDir(), "imap-ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KAWARIMI_IMAP_CA", caPath)

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("imap mock listen: %v", err)
	}
	s := &IMAPServer{ln: ln}
	s.Host, s.Port = splitHostPort(t, ln.Addr().String())

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return s
}

// ScriptAlive makes SEARCH return the given message IDs (an ALIVE reply exists).
func (s *IMAPServer) ScriptAlive(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aliveIDs = ids
}

// RejectLogin makes LOGIN fail (wrong credentials).
func (s *IMAPServer) RejectLogin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejectLogin = true
}

// Commands returns every client line received, in order.
func (s *IMAPServer) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

func (s *IMAPServer) handle(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	fmt.Fprintf(conn, "* OK kawarimi-testenv IMAP ready\r\n")

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.commands = append(s.commands, line)
		aliveIDs, reject := s.aliveIDs, s.rejectLogin
		s.mu.Unlock()

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tag, verb := fields[0], strings.ToUpper(fields[1])
		switch verb {
		case "LOGIN":
			if reject {
				fmt.Fprintf(conn, "%s NO LOGIN failed\r\n", tag)
			} else {
				fmt.Fprintf(conn, "%s OK LOGIN completed\r\n", tag)
			}
		case "SELECT":
			fmt.Fprintf(conn, "* 3 EXISTS\r\n%s OK SELECT completed\r\n", tag)
		case "SEARCH":
			if len(aliveIDs) > 0 {
				fmt.Fprintf(conn, "* SEARCH %s\r\n", strings.Join(aliveIDs, " "))
			} else {
				fmt.Fprintf(conn, "* SEARCH\r\n")
			}
			fmt.Fprintf(conn, "%s OK SEARCH completed\r\n", tag)
		case "LOGOUT":
			fmt.Fprintf(conn, "* BYE\r\n%s OK LOGOUT completed\r\n", tag)
			return
		default:
			fmt.Fprintf(conn, "%s BAD unknown command\r\n", tag)
		}
	}
}

// selfSignedCert generates a throwaway TLS cert for 127.0.0.1 and its PEM.
func selfSignedCert(t testing.TB) (tls.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kawarimi-testenv"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert, certPEM
}

func splitHostPort(t testing.TB, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	return host, port
}
