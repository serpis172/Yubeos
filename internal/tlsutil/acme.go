package tlsutil

import "golang.org/x/crypto/acme/autocert"

// NewACME returns a Let's Encrypt (ACME) certificate manager restricted to domains. Certificates are cached in dir
// (0700). TLS-ALPN-01 is answered on the HTTPS listener; HTTP-01 through Manager.HTTPHandler on the port-80 listener.
func NewACME(dir string, domains []string, email string) *autocert.Manager {
	return &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(dir),
		HostPolicy: autocert.HostWhitelist(domains...),
		Email:      email,
	}
}
