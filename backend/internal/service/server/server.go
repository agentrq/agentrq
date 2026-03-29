package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
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
		SSLCacheDir        string
		LetsencryptEmail   string
		CloudflareAPIToken string
	}

	Params struct {
		Config Config
		Router http.Handler
	}

	Service interface {
		Run() error
		Shutdown(ctx context.Context) error
	}

	service struct {
		cfg      Config
		router   http.Handler
		acmesrv  *http.Server
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
		cfg:    p.Config,
		router: p.Router,
	}, nil
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	domain := strings.ToLower(s.cfg.Domain)
	host := strings.ToLower(r.Host)
	if col := strings.IndexByte(host, ':'); col != -1 {
		host = host[:col]
	}

	// Skip hostname checks for localhost
	if domain != "" && !strings.HasPrefix(host, "localhost") && !strings.HasPrefix(host, "127.0.0.1") {
		mcpSuffix := ".mcp." + domain
		appHost := "app." + domain

		// 1. Check if it's an MCP subdomain
		if strings.HasSuffix(host, mcpSuffix) {
			workspaceID36 := strings.TrimSuffix(host, mcpSuffix)
			// Subdomain is in base36, parse it back to int64
			id, err := strconv.ParseInt(workspaceID36, 36, 64)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "Invalid workspace ID in subdomain: %s", workspaceID36)
				return
			}
			// Map it back to the base62 system for internal routing
			workspaceID := monoflake.ID(id).String()
			r.URL.Path = "/mcp/" + workspaceID
		} else if host != appHost {
			// 2. If it's not appHost or an MCP host, reject it
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "Host %s not allowed", host)
			return
		}
	}

	s.router.ServeHTTP(w, r)
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
	certFile := filepath.Join(s.cfg.SSLCacheDir, "combined.crt")
	keyFile := filepath.Join(s.cfg.SSLCacheDir, "combined.key")

	// 2. Load existing cert if available
	if err := s.loadCertificate(certFile, keyFile); err != nil {
		zlog.Warn().Err(err).Msg("could not load existing certificate, will obtain new one")
	}

	// 3. Start certificate manager (obtain/renew in background)
	go s.manageCertificates(certFile, keyFile)

	// 4. Start HTTP -> HTTPS Redirect Server
	go func() {
		zlog.Info().Str("addr", addr).Msg("starting HTTP redirect server")
		s.acmesrv = &http.Server{
			Addr: addr,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Strip port and ensure lowercase for check
				host := strings.ToLower(r.Host)
				if col := strings.IndexByte(host, ':'); col != -1 {
					host = host[:col]
				}

				mcpSuffix := ".mcp." + domain
				appHost := "app." + domain

				// Only redirect authorized hostnames
				if strings.HasSuffix(host, mcpSuffix) || host == appHost {
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
			appHost := "app." + domain

			if !strings.HasSuffix(serverName, mcpSuffix) && serverName != appHost {
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
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
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
	config.CADirURL = lego.LEDirectoryProduction
	config.Certificate.KeyType = certcrypto.EC256

	client, err := lego.NewClient(config)
	if err != nil {
		zlog.Error().Err(err).Msg("lego client creation failed")
		return
	}

	// Configure Cloudflare DNS Provider
	cfConfig := cloudflare.NewDefaultConfig()
	cfConfig.AuthToken = s.cfg.CloudflareAPIToken

	dnsProvider, err := cloudflare.NewDNSProviderConfig(cfConfig)
	if err != nil {
		zlog.Error().Err(err).Msg("cloudflare provider creation failed")
		return
	}

	if err := client.Challenge.SetDNS01Provider(dnsProvider); err != nil {
		zlog.Error().Err(err).Msg("failed to set DNS provider")
		return
	}

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		zlog.Error().Err(err).Msg("ACME registration failed")
		return
	}
	user.Registration = reg

	// Cert covers root, app subdomain, and nested MCP subdomains
	domains := []string{
		// domain,
		"app." + domain,
		"*.mcp." + domain,
	}

	for {
		needsRenewal := true
		s.certMu.RLock()
		if s.currCert != nil && len(s.currCert.Certificate) > 0 {
			x509Cert, err := x509.ParseCertificate(s.currCert.Certificate[0])
			if err == nil && time.Until(x509Cert.NotAfter) > 30*24*time.Hour {
				needsRenewal = false
			}
		}
		s.certMu.RUnlock()

		if needsRenewal {
		zlog.Info().Strs("domains", domains).Msg("obtaining/renewing certificate")

			request := certificate.ObtainRequest{
				Domains: domains,
				Bundle:  true,
			}

			certs, err := client.Certificate.Obtain(request)
			if err != nil {
				zlog.Error().Err(err).Msg("failed to obtain certificate")
			} else {
				if err := os.WriteFile(certPath, certs.Certificate, 0600); err != nil {
					zlog.Error().Err(err).Msg("failed to save cert file")
				}
				if err := os.WriteFile(keyPath, certs.PrivateKey, 0600); err != nil {
					zlog.Error().Err(err).Msg("failed to save key file")
				}

				if err := s.loadCertificate(certPath, keyPath); err != nil {
					zlog.Error().Err(err).Msg("failed to reload certificate")
				} else {
					zlog.Info().Msg("certificate updated successfully")
				}
			}
		} else {
			zlog.Info().Msg("certificate still valid, skipping renewal")
		}

		time.Sleep(24 * time.Hour)
	}
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
