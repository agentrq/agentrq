// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certificate"
)

var testDomains = certDomains("example.com")

// newTestPair is a self-signed certificate for names, valid for validFor.
func newTestPair(t *testing.T, names []string, validFor time.Duration) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validFor),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// fakeCertStore is an in-memory certificate bucket.
type fakeCertStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	getErr  map[string]error
	putErr  map[string]error
	puts    []string
}

func newFakeCertStore() *fakeCertStore {
	return &fakeCertStore{objects: map[string][]byte{}, getErr: map[string]error{}, putErr: map[string]error{}}
}

func (f *fakeCertStore) Get(_ context.Context, namespace, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := namespace + "/" + key
	if err := f.getErr[k]; err != nil {
		return nil, err
	}
	data, ok := f.objects[k]
	if !ok {
		return nil, errors.New("NoSuchKey: the object does not exist")
	}
	return data, nil
}

func (f *fakeCertStore) PutPrivate(_ context.Context, namespace, key string, data []byte, contentType string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := namespace + "/" + key
	if err := f.putErr[k]; err != nil {
		return "", err
	}
	f.puts = append(f.puts, k+" "+contentType)
	f.objects[k] = data
	return "etag", nil
}

// fakeObtainer stands in for the CA.
type fakeObtainer struct {
	res      *certificate.Resource
	err      error
	requests []certificate.ObtainRequest
	called   chan struct{}
}

func (f *fakeObtainer) Obtain(req certificate.ObtainRequest) (*certificate.Resource, error) {
	f.requests = append(f.requests, req)
	if f.called != nil {
		f.called <- struct{}{}
	}
	return f.res, f.err
}

func newCertService(store CertStore, obtainer certObtainer) *service {
	return &service{cfg: Config{Domain: "Example.com"}, certStore: store, obtainer: obtainer}
}

func certPaths(t *testing.T) (string, string) {
	dir := t.TempDir()
	return filepath.Join(dir, certFileName), filepath.Join(dir, keyFileName)
}

func servedNames(t *testing.T, s *service) []string {
	t.Helper()
	s.certMu.RLock()
	defer s.certMu.RUnlock()
	if s.currCert == nil {
		return nil
	}
	leaf, err := x509.ParseCertificate(s.currCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf.DNSNames
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// 1. A valid certificate on disk is served as it is: the bucket and the CA
// are never asked.
func TestRefreshCertificateServesTheLocalOneWhileValid(t *testing.T) {
	certPath, keyPath := certPaths(t)
	certPEM, keyPEM := newTestPair(t, testDomains, 60*24*time.Hour)
	os.WriteFile(certPath, certPEM, 0600)
	os.WriteFile(keyPath, keyPEM, 0600)

	store := newFakeCertStore()
	store.getErr["certs/example.com/"+certFileName] = errors.New("the bucket must not be read")
	obtainer := &fakeObtainer{err: errors.New("the CA must not be asked")}
	s := newCertService(store, obtainer)
	if err := s.loadCertificate(certPath, keyPath); err != nil {
		t.Fatal(err)
	}

	s.refreshCertificate(context.Background(), certPath, keyPath, testDomains)

	if len(obtainer.requests) != 0 || len(store.puts) != 0 {
		t.Errorf("a valid local certificate went to the CA (%d) or the bucket (%v)", len(obtainer.requests), store.puts)
	}
}

// 2. With nothing usable on disk, the bucket's certificate is written to
// disk and served, and the CA is not asked.
func TestRefreshCertificateRestoresFromTheBucket(t *testing.T) {
	certPath, keyPath := certPaths(t)
	certPEM, keyPEM := newTestPair(t, testDomains, 60*24*time.Hour)
	store := newFakeCertStore()
	store.objects["certs/example.com/"+certFileName] = certPEM
	store.objects["certs/example.com/"+keyFileName] = keyPEM
	obtainer := &fakeObtainer{err: errors.New("the CA must not be asked")}
	s := newCertService(store, obtainer)

	s.refreshCertificate(context.Background(), certPath, keyPath, testDomains)

	if len(obtainer.requests) != 0 {
		t.Error("the CA was asked although the bucket held a valid certificate")
	}
	if !bytes.Equal(readFile(t, certPath), certPEM) || !bytes.Equal(readFile(t, keyPath), keyPEM) {
		t.Error("the bucket's certificate was not written to disk")
	}
	if got := servedNames(t, s); len(got) != len(testDomains) {
		t.Errorf("serving %v, want the bucket's certificate", got)
	}
	if len(store.puts) != 0 {
		t.Errorf("a restored certificate was uploaded again: %v", store.puts)
	}
}

// 3. Otherwise a new certificate is obtained, written to disk, served and
// saved to the bucket.
func TestRefreshCertificateObtainsWhenTheBucketHasNoUsableOne(t *testing.T) {
	expCert, expKey := newTestPair(t, testDomains, 10*24*time.Hour)
	otherCert, otherKey := newTestPair(t, []string{"app.other.com"}, 60*24*time.Hour)
	_, strayKey := newTestPair(t, testDomains, 60*24*time.Hour)
	for _, tt := range []struct {
		name  string
		setup func(*fakeCertStore)
	}{
		{"Empty", func(*fakeCertStore) {}},
		{"Expiring", func(f *fakeCertStore) {
			f.objects["certs/example.com/"+certFileName] = expCert
			f.objects["certs/example.com/"+keyFileName] = expKey
		}},
		{"OtherDomain", func(f *fakeCertStore) {
			f.objects["certs/example.com/"+certFileName] = otherCert
			f.objects["certs/example.com/"+keyFileName] = otherKey
		}},
		{"KeyMissing", func(f *fakeCertStore) {
			f.objects["certs/example.com/"+certFileName] = expCert
		}},
		{"KeyDoesNotMatch", func(f *fakeCertStore) {
			f.objects["certs/example.com/"+certFileName] = otherCert
			f.objects["certs/example.com/"+keyFileName] = strayKey
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			certPath, keyPath := certPaths(t)
			certPEM, keyPEM := newTestPair(t, testDomains, 90*24*time.Hour)
			store := newFakeCertStore()
			tt.setup(store)
			obtainer := &fakeObtainer{res: &certificate.Resource{Certificate: certPEM, PrivateKey: keyPEM}}
			s := newCertService(store, obtainer)

			s.refreshCertificate(context.Background(), certPath, keyPath, testDomains)

			if len(obtainer.requests) != 1 || !obtainer.requests[0].Bundle || len(obtainer.requests[0].Domains) != len(testDomains) {
				t.Fatalf("CA requests: %+v", obtainer.requests)
			}
			if !bytes.Equal(readFile(t, certPath), certPEM) || !bytes.Equal(readFile(t, keyPath), keyPEM) {
				t.Error("the new certificate was not written to disk")
			}
			if !bytes.Equal(store.objects["certs/example.com/"+certFileName], certPEM) || !bytes.Equal(store.objects["certs/example.com/"+keyFileName], keyPEM) {
				t.Error("the new certificate was not saved to the bucket")
			}
			if want := "certs/example.com/" + certFileName + " application/x-pem-file"; store.puts[0] != want {
				t.Errorf("uploaded %q, want %q", store.puts[0], want)
			}
			if servedNames(t, s) == nil {
				t.Error("the new certificate is not served")
			}
		})
	}
}

func TestRefreshCertificateWithoutABucket(t *testing.T) {
	certPath, keyPath := certPaths(t)
	certPEM, keyPEM := newTestPair(t, testDomains, 90*24*time.Hour)
	obtainer := &fakeObtainer{res: &certificate.Resource{Certificate: certPEM, PrivateKey: keyPEM}}
	s := newCertService(nil, obtainer)

	s.refreshCertificate(context.Background(), certPath, keyPath, testDomains)

	if len(obtainer.requests) != 1 || servedNames(t, s) == nil {
		t.Error("with no bucket, the certificate was not obtained and served")
	}
}

func TestRefreshCertificateFailures(t *testing.T) {
	certPEM, keyPEM := newTestPair(t, testDomains, 90*24*time.Hour)

	t.Run("CAUnavailable", func(t *testing.T) {
		certPath, keyPath := certPaths(t)
		store := newFakeCertStore()
		s := newCertService(store, &fakeObtainer{err: errors.New("CA unavailable")})
		s.refreshCertificate(context.Background(), certPath, keyPath, testDomains)
		if servedNames(t, s) != nil || len(store.puts) != 0 {
			t.Error("a failed request still served or uploaded a certificate")
		}
	})

	t.Run("CAReturnsGarbage", func(t *testing.T) {
		certPath, keyPath := certPaths(t)
		store := newFakeCertStore()
		s := newCertService(store, &fakeObtainer{res: &certificate.Resource{Certificate: []byte("not a certificate"), PrivateKey: keyPEM}})
		s.refreshCertificate(context.Background(), certPath, keyPath, testDomains)
		if _, err := os.Stat(certPath); err == nil {
			t.Error("a certificate that does not parse was written to disk")
		}
		if len(store.puts) != 0 {
			t.Errorf("a certificate that does not parse was uploaded: %v", store.puts)
		}
	})

	t.Run("DiskUnwritable", func(t *testing.T) {
		dir := t.TempDir()
		store := newFakeCertStore()
		s := newCertService(store, &fakeObtainer{res: &certificate.Resource{Certificate: certPEM, PrivateKey: keyPEM}})
		s.refreshCertificate(context.Background(), filepath.Join(dir, "missing", certFileName), filepath.Join(dir, keyFileName), testDomains)
		if servedNames(t, s) != nil || len(store.puts) != 0 {
			t.Error("a certificate that could not be written was served or uploaded")
		}
	})

	t.Run("KeyUnwritable", func(t *testing.T) {
		dir := t.TempDir()
		s := newCertService(nil, &fakeObtainer{res: &certificate.Resource{Certificate: certPEM, PrivateKey: keyPEM}})
		s.refreshCertificate(context.Background(), filepath.Join(dir, certFileName), filepath.Join(dir, "missing", keyFileName), testDomains)
		if servedNames(t, s) != nil {
			t.Error("a certificate whose key could not be written is served")
		}
	})

	for _, key := range []string{certFileName, keyFileName} {
		t.Run("BucketUploadFails/"+key, func(t *testing.T) {
			certPath, keyPath := certPaths(t)
			store := newFakeCertStore()
			store.putErr["certs/example.com/"+key] = errors.New("bucket unavailable")
			s := newCertService(store, &fakeObtainer{res: &certificate.Resource{Certificate: certPEM, PrivateKey: keyPEM}})
			s.refreshCertificate(context.Background(), certPath, keyPath, testDomains)
			// The bucket is a copy: the certificate is served all the same.
			if servedNames(t, s) == nil {
				t.Error("a failed upload stopped the certificate from being served")
			}
		})
	}
}

func TestCertificateUsable(t *testing.T) {
	s := &service{}
	certPEM, keyPEM := newTestPair(t, testDomains, 60*24*time.Hour)
	valid, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		cert *tls.Certificate
		want bool
	}{
		{"Nil", nil, false},
		{"Empty", &tls.Certificate{}, false},
		{"Unparsable", &tls.Certificate{Certificate: [][]byte{[]byte("not DER")}}, false},
		{"Valid", &valid, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.certificateUsable(tt.cert, testDomains); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// fakeACME is just enough of an ACME server for an account to register.
// Orders are refused.
func fakeACME(t *testing.T, accountStatus int) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Replay-Nonce", fmt.Sprintf("nonce-%d", time.Now().UnixNano()))
		switch r.URL.Path {
		case "/directory":
			fmt.Fprintf(w, `{"newNonce":%q,"newAccount":%q,"newOrder":%q,"revokeCert":%q,"keyChange":%q}`,
				srv.URL+"/nonce", srv.URL+"/account", srv.URL+"/order", srv.URL+"/revoke", srv.URL+"/key")
		case "/nonce":
			w.WriteHeader(http.StatusOK)
		case "/account":
			w.Header().Set("Content-Type", "application/json")
			if accountStatus != http.StatusCreated {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(accountStatus)
				fmt.Fprint(w, `{"type":"urn:ietf:params:acme:error:malformed","detail":"registration refused"}`)
				return
			}
			w.Header().Set("Location", srv.URL+"/account/1")
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"status":"valid"}`)
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"type":"urn:ietf:params:acme:error:unauthorized","detail":"orders refused"}`)
		}
	}))
	t.Cleanup(srv.Close)
	// lego insists on HTTPS, and trusts the roots this variable names.
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGO_CA_CERTIFICATES", ca)
	return srv
}

func useACMEDirectory(t *testing.T, url string) {
	t.Helper()
	old := acmeDirectoryURL
	acmeDirectoryURL = url
	t.Cleanup(func() { acmeDirectoryURL = old })
	// A Cloudflare setting from the environment must not leak in.
	for _, k := range []string{"CLOUDFLARE_TTL", "CF_API_TOKEN", "CLOUDFLARE_DNS_API_TOKEN", "CLOUDFLARE_EMAIL", "CLOUDFLARE_API_KEY"} {
		t.Setenv(k, "")
	}
}

// The ACME account is set up on the first request, and kept for the next.
func TestObtainCertificateSetsUpTheACMEClientOnce(t *testing.T) {
	srv := fakeACME(t, http.StatusCreated)
	useACMEDirectory(t, srv.URL+"/directory")
	s := &service{cfg: Config{Domain: "example.com", CloudflareAPIToken: "cf-token"}}

	_, err := s.obtainCertificate(testDomains)
	if err == nil || !strings.Contains(err.Error(), "orders refused") {
		t.Fatalf("obtain: %v, want the fake CA's refusal of the order", err)
	}
	first := s.obtainer
	if first == nil {
		t.Fatal("the ACME client was not kept")
	}
	s.obtainCertificate(testDomains)
	if s.obtainer != first {
		t.Error("the ACME client was set up again")
	}
}

func TestNewACMEObtainerFailures(t *testing.T) {
	t.Run("DirectoryUnreachable", func(t *testing.T) {
		srv := fakeACME(t, http.StatusCreated)
		useACMEDirectory(t, srv.URL+"/no-directory-here")
		s := &service{cfg: Config{Domain: "example.com", LetsencryptEmail: "ops@example.com", CloudflareAPIToken: "cf-token"}}
		if _, err := s.obtainCertificate(testDomains); err == nil || !strings.Contains(err.Error(), "lego client creation failed") {
			t.Errorf("got %v", err)
		}
		if s.obtainer != nil {
			t.Error("a failed setup was kept")
		}
	})
	t.Run("CloudflareMisconfigured", func(t *testing.T) {
		srv := fakeACME(t, http.StatusCreated)
		useACMEDirectory(t, srv.URL+"/directory")
		t.Setenv("CLOUDFLARE_TTL", "1")
		s := &service{cfg: Config{Domain: "example.com", CloudflareAPIToken: "cf-token"}}
		if _, err := s.newACMEObtainer(); err == nil || !strings.Contains(err.Error(), "cloudflare provider creation failed") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("RegistrationRefused", func(t *testing.T) {
		srv := fakeACME(t, http.StatusBadRequest)
		useACMEDirectory(t, srv.URL+"/directory")
		s := &service{cfg: Config{Domain: "example.com", CloudflareAPIToken: "cf-token"}}
		if _, err := s.newACMEObtainer(); err == nil || !strings.Contains(err.Error(), "ACME registration failed") {
			t.Errorf("got %v", err)
		}
	})
}

// The certificate manager checks at once, then sleeps until the next day.
func TestManageCertificatesChecksAtOnce(t *testing.T) {
	certPath, keyPath := certPaths(t)
	// Refused, so nothing is written to the test's directory after it ends.
	obtainer := &fakeObtainer{err: errors.New("CA unavailable"), called: make(chan struct{}, 1)}
	s := newCertService(nil, obtainer)

	go s.manageCertificates(certPath, keyPath)

	select {
	case <-obtainer.called:
	case <-time.After(10 * time.Second):
		t.Fatal("the certificate manager did not check at start")
	}
	if got := obtainer.requests[0].Domains; strings.Join(got, ",") != "app.example.com,mcp.example.com,*.mcp.example.com" {
		t.Errorf("asked for %v", got)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// With SSL on, the certificate on disk is served over TLS from the start.
func TestRunServesTheLocalCertificateOverTLS(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM := newTestPair(t, testDomains, 90*24*time.Hour)
	os.WriteFile(filepath.Join(dir, certFileName), certPEM, 0600)
	os.WriteFile(filepath.Join(dir, keyFileName), keyPEM, 0600)

	port, httpPort := freePort(t), freePort(t)
	obtainer := &fakeObtainer{err: errors.New("the CA must not be asked")}
	s := &service{
		cfg:      Config{SSLEnabled: true, Port: httpPort, SSLPort: port, Domain: "example.com", SSLCacheDir: dir},
		router:   http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }),
		obtainer: obtainer,
	}
	done := make(chan error, 1)
	go func() { done <- s.Run() }()

	var conn *tls.Conn
	var err error
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err = tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port), &tls.Config{ServerName: "app.example.com", InsecureSkipVerify: true})
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("TLS handshake: %v", err)
	}
	names := conn.ConnectionState().PeerCertificates[0].DNSNames
	conn.Close()
	if strings.Join(names, ",") != strings.Join(testDomains, ",") {
		t.Errorf("served %v", names)
	}

	// Plain HTTP redirects to HTTPS, for the app's own hosts only.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, tt := range []struct {
		host     string
		status   int
		location string
	}{
		{"APP.example.com:8080", http.StatusMovedPermanently, fmt.Sprintf("https://app.example.com:%d/x?y=1", port)},
		{"attacker.com", http.StatusNotFound, ""},
	} {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/x?y=1", httpPort), nil)
		req.Host = tt.host
		var res *http.Response
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
			if res, err = client.Do(req); err == nil || time.Now().After(deadline) {
				break
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tt.status || res.Header.Get("Location") != tt.location {
			t.Errorf("%s: %d %q, want %d %q", tt.host, res.StatusCode, res.Header.Get("Location"), tt.status, tt.location)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Run returned %v", err)
	}
}
