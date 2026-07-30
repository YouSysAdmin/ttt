package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	ttls "ttt/internal/core/tls"
	remoteserver "ttt/internal/remote/server"
)

func newServerCmd(app *App, version string) *cobra.Command {
	var listen, token string
	var tlsMode, tlsCert, tlsKey, tlsCA, tlsFQDN, tlsAlg string
	var acmeEmail, acmeCacheDir, acmeHTTPAddr string
	var acmeHosts []string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Serve the local database to remote ttt clients",
		Long: "Serve the local database over HTTP/JSON so other workstations can\n" +
			"use it by setting remote.url/remote.token (or TTT_REMOTE_URL /\n" +
			"TTT_REMOTE_TOKEN, or --remote-url/--remote-token).\n\n" +
			"A bearer token is required; the server refuses to start without one.\n" +
			"TLS modes: none (plain HTTP, for localhost/VPN), manual (--tls-cert/\n" +
			"--tls-key), self-signed (generated at startup; clients need\n" +
			"--remote-insecure), mutual (client certificates required), acme\n" +
			"(Let's Encrypt; needs public DNS and the challenge port).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := app.rt.Config
			// Flags outrank config, matching the root --db convention.
			if !cmd.Flags().Changed("listen") && cfg.Server.Listen != "" {
				listen = cfg.Server.Listen
			}
			if token == "" {
				token = cfg.Server.Token
			}
			if token == "" {
				return errors.New("a token is required (--token, server.token, or TTT_SERVER_TOKEN): never serve the database unauthenticated")
			}
			if !cmd.Flags().Changed("tls-mode") && cfg.Server.TLS.Mode != "" {
				tlsMode = cfg.Server.TLS.Mode
			}
			pick := func(flag, cfgVal string) string {
				if flag != "" {
					return flag
				}
				return cfgVal
			}
			tlsCert = pick(tlsCert, cfg.Server.TLS.Cert)
			tlsKey = pick(tlsKey, cfg.Server.TLS.Key)
			tlsCA = pick(tlsCA, cfg.Server.TLS.CA)
			tlsFQDN = pick(tlsFQDN, cfg.Server.TLS.FQDN)
			tlsAlg = pick(tlsAlg, cfg.Server.TLS.Alg)
			acmeEmail = pick(acmeEmail, cfg.Server.ACME.Email)
			acmeCacheDir = pick(acmeCacheDir, cfg.Server.ACME.CacheDir)
			acmeHTTPAddr = pick(acmeHTTPAddr, cfg.Server.ACME.HTTPAddr)
			if len(acmeHosts) == 0 {
				acmeHosts = cfg.Server.ACME.Hosts
			}

			tlsCfg, err := serverTLSConfig(tlsMode, ttls.ManualTLS{CertFile: tlsCert, KeyFile: tlsKey},
				ttls.MutualTLS{CertFile: tlsCert, KeyFile: tlsKey, CAFile: tlsCA},
				tlsFQDN, tlsAlg,
				ttls.ACME{Enable: true, Email: acmeEmail, Hosts: acmeHosts, CacheDir: acmeCacheDir, HTTPAddr: acmeHTTPAddr})
			if err != nil {
				return err
			}

			srv := &http.Server{
				Addr:      listen,
				Handler:   remoteserver.New(app.st, token, version),
				TLSConfig: tlsCfg,
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			errCh := make(chan error, 1)
			go func() {
				var err error
				if tlsCfg != nil {
					// Certificates come from TLSConfig, so no file arguments.
					err = srv.ListenAndServeTLS("", "")
				} else {
					err = srv.ListenAndServe()
				}
				if !errors.Is(err, http.ErrServerClosed) {
					errCh <- err
				}
			}()

			scheme := "http"
			if tlsCfg != nil {
				scheme = "https"
			}
			cmd.Printf("Serving %s on %s://%s (tls: %s)\n", cfg.Database.Path, scheme, listen, tlsMode)

			select {
			case err := <-errCh:
				return err
			case <-ctx.Done():
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return srv.Shutdown(shutdownCtx)
		},
	}

	cmd.Flags().StringVar(&listen, "listen", ":8320", "address to listen on")
	cmd.Flags().StringVar(&token, "token", "", "bearer token clients must present (required; or server.token / TTT_SERVER_TOKEN)")
	cmd.Flags().StringVar(&tlsMode, "tls-mode", "none", "TLS mode: none, manual, self-signed, mutual, acme")
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "certificate file (manual, mutual)")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "private key file (manual, mutual)")
	cmd.Flags().StringVar(&tlsCA, "tls-ca", "", "CA bundle for verifying client certificates (mutual)")
	cmd.Flags().StringVar(&tlsFQDN, "tls-fqdn", "localhost", "hostname for the generated certificate (self-signed)")
	cmd.Flags().StringVar(&tlsAlg, "tls-alg", "rsa", "key algorithm for the generated certificate: rsa or ed25519 (self-signed)")
	cmd.Flags().StringVar(&acmeEmail, "acme-email", "", "contact email for Let's Encrypt (acme)")
	cmd.Flags().StringSliceVar(&acmeHosts, "acme-hosts", nil, "hostnames allowed to request certificates (acme, required)")
	cmd.Flags().StringVar(&acmeCacheDir, "acme-cache-dir", "", "directory for cached certificates (acme; default: certs)")
	cmd.Flags().StringVar(&acmeHTTPAddr, "acme-http-addr", "", "address for the HTTP challenge listener (acme; default: :80)")
	return cmd
}

// serverTLSConfig maps --tls-mode to a *tls.Config via internal/core/tls.
// A nil config means plain HTTP.
func serverTLSConfig(mode string, manual ttls.ManualTLS, mutual ttls.MutualTLS, fqdn, alg string, acme ttls.ACME) (*tls.Config, error) {
	switch mode {
	case "", "none":
		return nil, nil
	case "manual":
		if manual.CertFile == "" || manual.KeyFile == "" {
			return nil, errors.New("tls-mode manual requires --tls-cert and --tls-key")
		}
		return ttls.LoadManualTLS(manual)
	case "self-signed":
		return ttls.SelfSignedTLS(fqdn, alg)
	case "mutual":
		if mutual.CertFile == "" || mutual.KeyFile == "" || mutual.CAFile == "" {
			return nil, errors.New("tls-mode mutual requires --tls-cert, --tls-key, and --tls-ca")
		}
		server, _, err := ttls.LoadMutualTLS(mutual)
		return server, err
	case "acme":
		// Without a host whitelist autocert would attempt issuance for any
		// SNI name an unauthenticated peer sends (nil HostPolicy = allow
		// all) - fail closed like the other modes.
		if len(acme.Hosts) == 0 {
			return nil, errors.New("tls-mode acme requires --acme-hosts")
		}
		return ttls.AutoTLS(acme), nil
	default:
		return nil, fmt.Errorf("unknown tls-mode %q (none, manual, self-signed, mutual, acme)", mode)
	}
}
