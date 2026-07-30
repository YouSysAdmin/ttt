package cli

import (
	"strings"
	"testing"

	ttls "ttt/internal/core/tls"
)

// Acme must fail closed without a host whitelist: a nil autocert HostPolicy
// would let any unauthenticated peer drive issuance for arbitrary SNI names.
func TestServerTLSConfigAcmeRequiresHosts(t *testing.T) {
	_, err := serverTLSConfig("acme", ttls.ManualTLS{}, ttls.MutualTLS{}, "", "",
		ttls.ACME{Enable: true, Email: "a@b.c"})
	if err == nil || !strings.Contains(err.Error(), "acme-hosts") {
		t.Fatalf("expected acme-hosts error, got %v", err)
	}
}

func TestServerTLSConfigUnknownMode(t *testing.T) {
	_, err := serverTLSConfig("bogus", ttls.ManualTLS{}, ttls.MutualTLS{}, "", "", ttls.ACME{})
	if err == nil || !strings.Contains(err.Error(), "unknown tls-mode") {
		t.Fatalf("expected unknown-mode error, got %v", err)
	}
}
