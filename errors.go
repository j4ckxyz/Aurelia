package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"

	"aurelia/internal/jellyfin"
)

// friendly words an error for a person: which step of the way failed,
// the proxy or the server, how, and what that usually means. An error
// that is not of the network is told as it is.
func friendly(err error) string {
	if err == nil {
		return ""
	}
	host, hostPort := asked(err)
	via := proxy.Load()
	if via != nil && local(host) {
		via = nil // reached directly, whatever the setting
	}

	var status *jellyfin.StatusError
	var notJellyfin *jellyfin.NotJellyfinError
	switch {
	case errors.Is(err, jellyfin.ErrUnauthorized):
		return "The server refused the sign-in: the user name or the password is wrong, or the sign-in has ended."
	case errors.As(err, &status):
		return answered(status, via != nil)
	case errors.As(err, &notJellyfin):
		who := nameOf(notJellyfin.Server)
		if notJellyfin.Page {
			return who + " answered with a web page, not as a Jellyfin server does. Check the address; a network that blocks a site, or asks you to sign in to it first, shows its own page in the site's place."
		}
		return who + " answered, but not as a Jellyfin server does. Check the address, and the path if the server is under one, as in https://example.com/jellyfin."
	}

	// The proxy: not reached, or not letting this through.
	var op *net.OpError
	if errors.As(err, &op) {
		switch op.Op {
		case "proxyconnect":
			return proxyUnreached(op.Err)
		case "socks connect":
			return "The proxy was reached, but it did not connect to " + hostPort + ": " + bare(op.Err) + "."
		}
	}
	if text := innermost(err).Error(); via != nil && isStatusText(text) {
		// What a proxy answers a request for a tunnel with is all the
		// error is: no server's answer comes as one.
		at := via.Host
		switch text {
		case http.StatusText(http.StatusProxyAuthRequired), http.StatusText(http.StatusUnauthorized):
			return "The proxy at " + at + " refused its user name or password. Copy the proxy's address again, the whole of it."
		case http.StatusText(http.StatusForbidden):
			return "The proxy at " + at + " was reached, but it does not connect to " + hostPort + ": it allows only the servers and ports it is set up for."
		case http.StatusText(http.StatusTooManyRequests):
			return "The proxy at " + at + " is turning this address away for a while, after too many wrong passwords. Try again in ten minutes."
		case http.StatusText(http.StatusBadGateway), http.StatusText(http.StatusGatewayTimeout), http.StatusText(http.StatusServiceUnavailable):
			return "The proxy at " + at + " was reached, but it could not reach " + hostPort + " (" + text + "): the server may be off, or its address wrong."
		}
		return "The proxy at " + at + " answered \"" + text + "\" when asked for " + hostPort + "."
	}

	through := ""
	if via != nil {
		through = " through the proxy"
	}
	var dns *net.DNSError
	var unknown x509.UnknownAuthorityError
	var otherName x509.HostnameError
	var invalid x509.CertificateInvalidError
	var badCert *tls.CertificateVerificationError
	var notTLS tls.RecordHeaderError
	switch {
	case errors.As(err, &dns):
		name := dns.Name
		if name == "" {
			name = host
		}
		if dns.IsNotFound {
			return "This network does not know the name " + name + ". Check the address for a slip; if it is right, the network may be blocking it."
		}
		return "The name " + name + " could not be looked up (" + bare(dns) + "): this computer may be offline, or the network is not answering."
	case errors.As(err, &unknown):
		by := ""
		if c := unknown.Cert; c != nil && c.Issuer.CommonName != "" {
			by = " (signed by " + c.Issuer.CommonName + ")"
		} else if c != nil && len(c.Issuer.Organization) > 0 {
			by = " (signed by " + c.Issuer.Organization[0] + ")"
		}
		return host + " showed a certificate this computer does not trust" + by + ". A network that inspects connections signs them itself; so does a server with a certificate of its own making."
	case errors.As(err, &otherName):
		return host + " showed a certificate made for another name: something else answers at that address, as a network's block page does."
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return host + "'s certificate has expired, or this computer's clock is wrong."
	case errors.As(err, &invalid), errors.As(err, &badCert):
		return host + "'s certificate was not accepted (" + bare(innermost(err)) + "). A network that inspects connections causes this; so does a certificate that has expired."
	case errors.As(err, &notTLS), strings.Contains(err.Error(), "HTTP response to HTTPS client"):
		return hostPort + " does not answer at https. Try the address with http:// in front, or another port."
	case refused(err):
		return hostPort + " refused the connection" + through + ": nothing is listening there. Check the port, and that the server is running."
	case noRoute(err):
		return "This computer has no way to " + hostPort + through + ": it may be offline, or the network does not lead there."
	case timedOut(err):
		if op != nil && op.Op == "dial" {
			return "Nothing answered at " + hostPort + " within " + fmt.Sprint(int(connectTimeout.Seconds())) + " seconds. The server may be off, or the address or the port wrong; a network that blocks it looks the same."
		}
		if via != nil {
			return "The proxy was reached, but no answer from " + host + " came through it in time. The server may be busy or off, or the connection slow."
		}
		return host + " was reached, but its answer did not come in time. The server may be busy, or the connection slow."
	case cut(err):
		return "The connection to " + hostPort + through + " was cut as it began. A network that blocks or inspects a site does this; so does a server that is restarting."
	}
	var asURL *url.Error
	if errors.As(err, &asURL) {
		return "Could not reach " + hostPort + through + ": " + bare(asURL.Err) + "."
	}
	return err.Error()
}

// proxyUnreached tells why the connection to the proxy itself was not
// made.
func proxyUnreached(err error) string {
	via := proxy.Load()
	if via == nil {
		return "The proxy could not be reached: " + bare(err) + "."
	}
	at := via.Host
	var dns *net.DNSError
	var badCert *tls.CertificateVerificationError
	var unknown x509.UnknownAuthorityError
	switch {
	case errors.Is(err, errPinMismatch):
		return "The proxy at " + at + " showed another certificate than the one its address names. This network may be reading connections on their way. If the proxy was given a new certificate, copy its address again."
	case errors.As(err, &dns):
		return "This network does not know the proxy's name, " + via.Hostname() + ". Check it for a slip; the network may also be blocking it."
	case errors.As(err, &unknown), errors.As(err, &badCert), strings.Contains(err.Error(), "x509:"):
		return "The proxy at " + at + " showed a certificate this computer does not trust. A proxy with a certificate of its own making needs its fingerprint in its address, as aurelia-proxy gives it."
	case refused(err):
		return "The proxy's address, " + at + ", refused the connection: nothing is listening there. Check the port, and that the proxy is running."
	case noRoute(err):
		return "This computer has no way to the proxy at " + at + ": it may be offline."
	case timedOut(err):
		msg := "The proxy at " + at + " did not answer within " + fmt.Sprint(int(connectTimeout.Seconds())) + " seconds. This network may block that port or that address, or the proxy is not running."
		if port := via.Port(); port != "443" && port != "80" {
			msg += " Restricted networks often let only ports 443 and 80 through."
		}
		return msg
	case cut(err):
		return "The connection to the proxy at " + at + " was cut as it began. A network that blocks or inspects connections does this."
	}
	return "Could not connect to the proxy at " + at + ": " + bare(err) + "."
}

// answered tells of an answer that was not the one asked for.
func answered(e *jellyfin.StatusError, proxied bool) string {
	who := nameOf(e.Server)
	code := " (" + e.Status + ")"
	switch {
	case e.Code == http.StatusProxyAuthRequired:
		// Of a proxy asked for a server at http://, which it answers for.
		return "The proxy refused its user name or password" + code + ". Copy the proxy's address again, the whole of it."
	case e.Challenge:
		msg := "Cloudflare, in front of " + who + ", stopped the request with a check only a browser passes" + code + "."
		if proxied {
			return msg + " It does this to addresses it does not trust, a proxy's often among them: the server's owner can let the proxy's address through in Cloudflare."
		}
		return msg + " It does this to addresses it does not trust: the server's owner can let yours through in Cloudflare."
	case e.Code == http.StatusForbidden && e.Page:
		return who + " answered with a page that refuses" + code + ", not as a Jellyfin server does. A network that blocks a site shows such a page in its place; so does a firewall in front of the server."
	case e.Code == http.StatusForbidden:
		return "The server refused that" + code + ": this account may not be allowed it."
	case e.Code == http.StatusNotFound && e.What == "":
		return "Nothing of Jellyfin was found at " + e.Server + code + ". Check the address, and the path if the server is under one, as in https://example.com/jellyfin."
	case e.Code == http.StatusBadGateway, e.Code == http.StatusServiceUnavailable, e.Code == http.StatusGatewayTimeout,
		e.Code >= 520 && e.Code <= 530: // Cloudflare's, of a server it cannot reach
		return "What stands in front of " + who + " answered, but Jellyfin behind it did not" + code + ": the server may be off or restarting."
	case e.Code >= 500:
		return "The server met an error of its own" + code + ". Its log tells more."
	case e.Code == http.StatusTooManyRequests:
		return "The server is turning requests away for a while" + code + ": too many came at once."
	case e.Page:
		return who + " answered with a web page" + code + ", not as a Jellyfin server does. Check the address; a network may also be showing its own page in the server's place."
	}
	return "The server answered " + e.Status + "."
}

// asked gives the host an error's request was for, and the host with
// its port: "the server" when the error does not say.
func asked(err error) (host, hostPort string) {
	var u *url.Error
	if errors.As(err, &u) {
		if at, e := url.Parse(u.URL); e == nil && at.Hostname() != "" {
			port := at.Port()
			if port == "" {
				port = map[string]string{"http": "80", "https": "443"}[at.Scheme]
			}
			return at.Hostname(), net.JoinHostPort(at.Hostname(), port)
		}
	}
	return "the server", "the server"
}

func nameOf(server string) string {
	if u, err := url.Parse(server); err == nil && u.Host != "" {
		return u.Host
	}
	return "the server"
}

func innermost(err error) error {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err
		}
		err = next
	}
}

// bare is an error without what Go puts in front of it, the addresses
// of both ends among it: "connection reset by peer" of "read tcp
// 10.0.0.2:51512->203.0.113.7:443: read: connection reset by peer".
func bare(err error) string {
	return strings.TrimSuffix(innermost(err).Error(), ".")
}

func isStatusText(s string) bool {
	for code := 300; code < 600; code++ {
		if t := http.StatusText(code); t != "" && t == s {
			return true
		}
	}
	return false
}

func timedOut(err error) bool {
	var t interface{ Timeout() bool }
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &t) && t.Timeout()
}

// What the systems say differs: Windows words its own.
func refused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(err.Error(), "refused")
}

func noRoute(err error) bool {
	msg := err.Error()
	return errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) ||
		strings.Contains(msg, "unreachable") || strings.Contains(msg, "no route to host")
}

func cut(err error) bool {
	msg := err.Error()
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || strings.HasSuffix(msg, "EOF") ||
		strings.Contains(msg, "reset by peer") || strings.Contains(msg, "forcibly closed") ||
		strings.Contains(msg, "connection was aborted") || strings.Contains(msg, "handshake failure")
}
