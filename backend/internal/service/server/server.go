// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package server

import (
	"bufio"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"strconv"

	zlog "github.com/rs/zerolog/log"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/registration"
	"github.com/mustafaturan/monoflake"
)

type (
	Config struct {
		Port               int
		SSLPort            int
		SSLEnabled         bool
		Domain             string
		ProxyDomain        string
		SSLCacheDir        string
		LetsencryptEmail   string
		CloudflareAPIToken string
	}

	Params struct {
		Config Config
		Router http.Handler
		// Probes are the health probe handlers, by exact path. They run
		// ahead of the host check and the request log: a probe addresses
		// the instance by its own IP, and arrives every few seconds.
		Probes map[string]http.Handler
		// CertStore, when set, is the bucket certificates are shared
		// through: a certificate missing on disk is looked for there
		// before one is obtained, and a new one is saved to it.
		CertStore CertStore
	}

	// CertStore is the part of the S3 service certificates need.
	CertStore interface {
		Get(ctx context.Context, namespace, key string) ([]byte, error)
		PutPrivate(ctx context.Context, namespace, key string, data []byte, contentType string) (string, error)
	}

	certObtainer interface {
		Obtain(request certificate.ObtainRequest) (*certificate.Resource, error)
	}

	Service interface {
		Run() error
		Shutdown(ctx context.Context) error
	}

	service struct {
		cfg     Config
		router  http.Handler
		probes  map[string]http.Handler
		acmesrv *http.Server
		// certStore is nil when no certificate bucket is configured.
		certStore CertStore
		// obtainer is built on the first certificate request.
		obtainer certObtainer
		server   *http.Server
		certMu   sync.RWMutex
		currCert *tls.Certificate
	}

	legoUser struct {
		Email        string
		Registration *registration.Resource
		key          crypto.PrivateKey
	}
)

func (u *legoUser) GetEmail() string                        { return u.Email }
func (u *legoUser) GetRegistration() *registration.Resource { return u.Registration }
func (u *legoUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func New(p Params) (Service, error) {
	return &service{
		cfg:       p.Config,
		router:    p.Router,
		probes:    p.Probes,
		certStore: p.CertStore,
	}, nil
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if probe, ok := s.probes[r.URL.Path]; ok {
		probe.ServeHTTP(w, r)
		return
	}

	start := time.Now()
	sw := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}

	defer func() {
		duration := time.Since(start)
		zlog.Info().
			Str("method", r.Method).
			Str("host", r.Host).
			Str("path", r.URL.Path).
			Int("status", sw.status).
			Dur("duration", duration).
			Str("remote", r.RemoteAddr).
			Msg("http request")
	}()

	domain := strings.ToLower(s.cfg.Domain)
	host := strings.ToLower(r.Host)
	if col := strings.IndexByte(host, ':'); col != -1 {
		host = host[:col]
	}

	// Skip hostname checks for localhost and ngrok tunnels to ease local development/testing
	if domain != "" && !strings.HasPrefix(host, "localhost") && !strings.HasPrefix(host, "127.0.0.1") && !strings.HasSuffix(host, ".ngrok-free.app") && !strings.HasSuffix(host, ".ngrok.io") {
		mcpSuffix := ".mcp." + domain
		coreMCPHost := "mcp." + domain
		appHost := "app." + domain

		// 1. Check if it's the CoreMCP subdomain or matches the raw domain or proxy domain
		proxyDomain := strings.ToLower(s.cfg.ProxyDomain)
		if host == domain || (proxyDomain != "" && host == proxyDomain) {
			// Allow raw domain or proxy domain (e.g. example.com when served directly or behind a path-based proxy)
		} else if host == coreMCPHost {
			// Allow it through. Paths starting with /mcp are expected.
		} else if strings.HasSuffix(host, mcpSuffix) {
			// 2. Check if it's a workspace-specific MCP subdomain
			workspaceID36 := strings.TrimSuffix(host, mcpSuffix)
			// Subdomain is in base36, parse it back to int64
			id, err := strconv.ParseInt(workspaceID36, 36, 64)
			if err != nil {
				sw.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(sw, "Invalid workspace ID in subdomain: %s", workspaceID36)
				return
			}
			// Map it back to the base62 system for internal routing
			workspaceID := monoflake.ID(id).String()

			// acp-gateway reaches native ACP on the workspace's own MCP host;
			// its token names the workspace, so the path is left as it is.
			switch {
			case strings.HasPrefix(r.URL.Path, "/api/acp/"):
			case r.URL.Path == "/" || r.URL.Path == "":
				r.URL.Path = "/mcp/" + workspaceID
			default:
				r.URL.Path = "/mcp/" + workspaceID + r.URL.Path
			}
		} else if host != appHost {
			// 3. If it's not appHost, CoreMCP host, or an MCP host, reject it
			sw.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(sw, "Host %s not allowed", host)
			return
		}
	}

	s.router.ServeHTTP(sw, r)
}

func (s *service) Run() error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)

	if !s.cfg.SSLEnabled {
		zlog.Info().Str("addr", addr).Msg("starting unified HTTP server")
		s.server = &http.Server{
			Addr:    addr,
			Handler: s,
		}
		return s.server.ListenAndServe()
	}

	// ── SSL / HTTPS via Lego + Cloudflare DNS-01 ──────────────────────────
	domain := strings.ToLower(s.cfg.Domain)
	if domain == "" {
		return errors.New("SSL enabled but domain is not set in config")
	}
	email := s.cfg.LetsencryptEmail
	if email == "" {
		email = "admin@" + domain
	}
	zlog.Info().Str("domain", domain).Str("email", email).Msg("SSL enabled with Lego/Cloudflare DNS-01")

	// 1. Setup Certificate Storage
	if err := os.MkdirAll(s.cfg.SSLCacheDir, 0700); err != nil {
		return fmt.Errorf("failed to create ssl cache dir: %w", err)
	}
	certFile := filepath.Join(s.cfg.SSLCacheDir, certFileName)
	keyFile := filepath.Join(s.cfg.SSLCacheDir, keyFileName)

	// 2. Load existing cert if available
	if err := s.loadCertificate(certFile, keyFile); err != nil {
		zlog.Warn().Err(err).Msg("could not load existing certificate, will obtain new one")
	}

	// 3. Start certificate manager (obtain/renew in background)
	go s.manageCertificates(certFile, keyFile)

	// 4. Start HTTP -> HTTPS Redirect Server. It is set up before the
	// goroutine starts, so a Shutdown that comes first still sees it.
	s.acmesrv = &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Strip port and ensure lowercase for check
			host := strings.ToLower(r.Host)
			if col := strings.IndexByte(host, ':'); col != -1 {
				host = host[:col]
			}

			mcpSuffix := ".mcp." + domain
			coreMCPHost := "mcp." + domain
			appHost := "app." + domain

			// Only redirect authorized hostnames
			if strings.HasSuffix(host, mcpSuffix) || host == appHost || host == coreMCPHost {
				target := "https://" + host
				if s.cfg.SSLPort != 443 && s.cfg.SSLPort != 0 {
					target += fmt.Sprintf(":%d", s.cfg.SSLPort)
				}
				target += r.RequestURI
				http.Redirect(w, r, target, http.StatusMovedPermanently)
			} else {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(w, "Host %s not allowed for redirect", host)
			}
		}),
	}
	go func() {
		zlog.Info().Str("addr", addr).Msg("starting HTTP redirect server")
		if err := s.acmesrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zlog.Error().Err(err).Msg("HTTP redirect server error")
		}
	}()

	// 5. Start HTTPS Server with dynamic certificate
	tlsConfig := &tls.Config{
		ClientSessionCache: tls.NewLRUClientSessionCache(100),
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			s.certMu.RLock()
			defer s.certMu.RUnlock()
			if s.currCert == nil {
				return nil, errors.New("certificate not ready yet")
			}

			// Ensure requested host is in our allowed list
			serverName := strings.ToLower(hello.ServerName)
			mcpSuffix := ".mcp." + domain
			coreMCPHost := "mcp." + domain
			appHost := "app." + domain

			if !strings.HasSuffix(serverName, mcpSuffix) && serverName != appHost && serverName != coreMCPHost {
				return nil, fmt.Errorf("certificate request for unauthorized host: %s", serverName)
			}

			return s.currCert, nil
		},
		MinVersion: tls.VersionTLS12,
		CurvePreferences: []tls.CurveID{
			tls.CurveP521,
			tls.CurveP384,
			tls.CurveP256,
		},
		NextProtos: []string{"h2", "http/1.1"},
	}

	sslAddr := fmt.Sprintf(":%d", s.cfg.SSLPort)
	if s.cfg.SSLPort == 0 {
		sslAddr = ":443"
	}

	ln, err := tls.Listen("tcp", sslAddr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS listener setup failed: %w", err)
	}

	zlog.Info().Str("addr", sslAddr).Str("domain", domain).Msg("starting secure unified server")
	s.server = &http.Server{
		Handler: s,
	}
	return s.server.Serve(ln)
}

// certNamespace is where certificates are kept in the certificate bucket,
// one folder per domain, so deployments can share a bucket.
const certNamespace = "certs"

// The certificate and key file names, on disk and in the bucket.
const (
	certFileName = "combined.crt"
	keyFileName  = "combined.key"
)

// certRenewBefore is how long before expiry a certificate is replaced.
const certRenewBefore = 30 * 24 * time.Hour

// acmeDirectoryURL is the CA certificates are obtained from.
var acmeDirectoryURL = lego.LEDirectoryProduction

func (s *service) loadCertificate(certPath, keyPath string) error {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return err
	}
	s.certMu.Lock()
	s.currCert = &cert
	s.certMu.Unlock()
	return nil
}

func (s *service) manageCertificates(certPath, keyPath string) {
	domains := certDomains(strings.ToLower(s.cfg.Domain))
	for {
		s.refreshCertificate(context.Background(), certPath, keyPath, domains)
		time.Sleep(24 * time.Hour)
	}
}

// certDomains are the names the certificate covers: the app, CoreMCP and
// every workspace's MCP subdomain.
func certDomains(domain string) []string {
	return []string{
		"app." + domain,
		"mcp." + domain,
		"*.mcp." + domain,
	}
}

// refreshCertificate makes sure a usable certificate is served: the one on
// disk, else the one in the certificate bucket, else a new one from the CA,
// which is written to disk and to the bucket.
func (s *service) refreshCertificate(ctx context.Context, certPath, keyPath string, domains []string) {
	s.certMu.RLock()
	curr := s.currCert
	s.certMu.RUnlock()
	if s.certificateUsable(curr, domains) {
		zlog.Info().Msg("certificate still valid, skipping renewal")
		return
	}

	if s.certStore != nil {
		err := s.restoreCertificate(ctx, certPath, keyPath, domains)
		if err == nil {
			zlog.Info().Msg("certificate restored from the certificate bucket")
			return
		}
		zlog.Warn().Err(err).Msg("no usable certificate in the certificate bucket, will obtain new one")
	}

	zlog.Info().Strs("domains", domains).Msg("obtaining/renewing certificate")
	certs, err := s.obtainCertificate(domains)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to obtain certificate")
		return
	}
	if err := s.installCertificate(certPath, keyPath, certs.Certificate, certs.PrivateKey); err != nil {
		zlog.Error().Err(err).Msg("failed to install certificate")
		return
	}
	zlog.Info().Msg("certificate updated successfully")

	if s.certStore != nil {
		if err := s.storeCertificate(ctx, certs.Certificate, certs.PrivateKey); err != nil {
			zlog.Error().Err(err).Msg("failed to save certificate to the certificate bucket")
		}
	}
}

// certificateUsable reports whether cert covers domains and is not due for
// renewal.
func (s *service) certificateUsable(cert *tls.Certificate, domains []string) bool {
	if cert == nil || len(cert.Certificate) == 0 {
		return false
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return false
	}
	return time.Until(leaf.NotAfter) >= certRenewBefore && s.certificateCoversDomains(leaf, domains)
}

// restoreCertificate installs the bucket's certificate, if it is usable.
func (s *service) restoreCertificate(ctx context.Context, certPath, keyPath string, domains []string) error {
	ns := s.certStoreNamespace()
	certPEM, err := s.certStore.Get(ctx, ns, certFileName)
	if err != nil {
		return err
	}
	keyPEM, err := s.certStore.Get(ctx, ns, keyFileName)
	if err != nil {
		return err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}
	if !s.certificateUsable(&cert, domains) {
		return errors.New("certificate in the bucket is expiring or does not cover every domain")
	}
	return s.installCertificate(certPath, keyPath, certPEM, keyPEM)
}

// storeCertificate uploads the certificate and its key to the bucket.
func (s *service) storeCertificate(ctx context.Context, certPEM, keyPEM []byte) error {
	ns := s.certStoreNamespace()
	if _, err := s.certStore.PutPrivate(ctx, ns, certFileName, certPEM, "application/x-pem-file"); err != nil {
		return err
	}
	_, err := s.certStore.PutPrivate(ctx, ns, keyFileName, keyPEM, "application/x-pem-file")
	return err
}

func (s *service) certStoreNamespace() string {
	return certNamespace + "/" + strings.ToLower(s.cfg.Domain)
}

// installCertificate writes the pair to disk and serves it. A pair that does
// not parse is refused before the files on disk are touched.
func (s *service) installCertificate(certPath, keyPath string, certPEM, keyPEM []byte) error {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return err
	}
	s.certMu.Lock()
	s.currCert = &cert
	s.certMu.Unlock()
	return nil
}

// obtainCertificate asks the CA for a certificate. The ACME client is set up,
// and the account registered, only the first time one is needed.
func (s *service) obtainCertificate(domains []string) (*certificate.Resource, error) {
	if s.obtainer == nil {
		obtainer, err := s.newACMEObtainer()
		if err != nil {
			return nil, err
		}
		s.obtainer = obtainer
	}
	return s.obtainer.Obtain(certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	})
}

func (s *service) newACMEObtainer() (certObtainer, error) {
	domain := strings.ToLower(s.cfg.Domain)
	email := s.cfg.LetsencryptEmail
	if email == "" {
		email = "admin@" + domain
	}

	privateKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	user := &legoUser{
		Email: email,
		key:   privateKey,
	}

	config := lego.NewConfig(user)
	config.CADirURL = acmeDirectoryURL
	config.Certificate.KeyType = certcrypto.EC256

	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("lego client creation failed: %w", err)
	}

	// Configure Cloudflare DNS Provider
	cfConfig := cloudflare.NewDefaultConfig()
	cfConfig.AuthToken = s.cfg.CloudflareAPIToken

	dnsProvider, err := cloudflare.NewDNSProviderConfig(cfConfig)
	if err != nil {
		return nil, fmt.Errorf("cloudflare provider creation failed: %w", err)
	}

	var reg *registration.Resource
	err = client.Challenge.SetDNS01Provider(dnsProvider)
	if err == nil {
		reg, err = client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	}
	if err != nil {
		return nil, fmt.Errorf("ACME registration failed: %w", err)
	}
	user.Registration = reg

	return client.Certificate, nil
}

func (s *service) Shutdown(ctx context.Context) error {
	if s.acmesrv != nil {
		_ = s.acmesrv.Shutdown(ctx)
	}
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *service) certificateCoversDomains(cert *x509.Certificate, domains []string) bool {
	for _, d := range domains {
		found := false
		for _, name := range cert.DNSNames {
			if name == d {
				found = true
				break
			}
		}
		if !found {
			zlog.Info().Str("missing_domain", d).Msg("certificate does not cover required domain, triggering renewal")
			return false
		}
	}
	return true
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack hands the raw connection to a caller that needs it.
//
// Wrapping a ResponseWriter hides every optional interface the original
// implemented, and net/http decides what a handler may do by type-asserting
// for exactly those. Flush above is the same story: without it, every
// streaming response buffers. Without this one, a WebSocket upgrade fails with
// "response does not implement http.Hijacker" and the client sees a bare 500 —
// which is how the daemon socket first behaved, with nothing in the log to say
// why.
//
// The status is left as-is deliberately. Once the connection is hijacked this
// wrapper is no longer in the path, and a hijacked request has no meaningful
// status to record: the upgrade has already been written by whoever took the
// connection.
func (w *statusResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("server: the underlying ResponseWriter cannot be hijacked")
	}
	return h.Hijack()
}
