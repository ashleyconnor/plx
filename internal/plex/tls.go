package plex

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
)

// Plex servers serve a publicly trusted wildcard certificate for
// *.<hash>.plex.direct, where <ip-with-dashes>.<hash>.plex.direct resolves
// to the server's IP. Connecting to the bare IP fails ordinary verification
// (the certificate names no IPs), so for IP URLs plx checks the certificate
// as if the matching plex.direct name had been used.

// tlsConfigFor returns the TLS config for baseURL: none needed for names,
// plex.direct verification for IP addresses, or none at all if insecure.
func tlsConfigFor(baseURL string, insecure bool) *tls.Config {
	if insecure {
		return &tls.Config{InsecureSkipVerify: true}
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil {
		return nil
	}
	return &tls.Config{
		InsecureSkipVerify: true, // replaced by VerifyConnection
		VerifyConnection:   func(cs tls.ConnectionState) error { return verifyPlexDirect(cs, ip) },
	}
}

var errNotPlexDirect = errors.New("the server's certificate isn't a valid plex.direct certificate for this address (set insecure = true to skip verification)")

// isCertError reports a failed certificate check, which retrying won't fix.
func isCertError(err error) bool {
	var verr *tls.CertificateVerificationError
	var uerr x509.UnknownAuthorityError
	var herr x509.HostnameError
	return errors.Is(err, errNotPlexDirect) || errors.As(err, &verr) || errors.As(err, &uerr) || errors.As(err, &herr)
}

func verifyPlexDirect(cs tls.ConnectionState, ip net.IP) error {
	if len(cs.PeerCertificates) == 0 {
		return errNotPlexDirect
	}
	leaf := cs.PeerCertificates[0]
	inter := x509.NewCertPool()
	for _, c := range cs.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Intermediates: inter}); err != nil {
		return err
	}
	dashed := strings.NewReplacer(".", "-", ":", "-").Replace(ip.String())
	for _, name := range leaf.DNSNames {
		if rest, ok := strings.CutPrefix(name, "*."); ok && strings.HasSuffix(rest, ".plex.direct") {
			if leaf.VerifyHostname(dashed+"."+rest) == nil {
				return nil
			}
		}
	}
	return errNotPlexDirect
}
