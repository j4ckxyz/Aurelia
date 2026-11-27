// Command aurelia-relay lets Aurelia's Jams be joined from anywhere. A
// Jam's host connects out to it; guests on any network reach the host
// through it. It keeps nothing: it carries what the host and its guests
// say, and the songs a guest plays from the host, while the Jam lasts.
//
// It is small: a few megabytes of memory, and the bandwidth of the songs
// that pass through it. Run it behind a reverse proxy that gives it HTTPS,
// as Caddy, or with -domain, which gets a certificate of its own.
//
//	AURELIA_RELAY_KEY=$(openssl rand -hex 24) aurelia-relay -addr 127.0.0.1:8787
//
// Hosts need the key; guests do not: the Jam's code, or its host's yes,
// lets them in, as on the local network.
package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

func main() {
	addr := flag.String("addr", ":8787", "where to listen for HTTP: behind a reverse proxy, as 127.0.0.1:8787")
	domain := flag.String("domain", "", "serve HTTPS on :443 for this name itself, with a certificate from Let's Encrypt (and :80 for its challenges)")
	certs := flag.String("certs", "certs", "where -domain keeps its certificates")
	maxRooms := flag.Int("max-rooms", 32, "how many Jams the relay carries at once")
	trustProxy := flag.Bool("behind-proxy", false, "a reverse proxy is in front: a client's address is the last of X-Forwarded-For")
	flag.Parse()

	key := os.Getenv("AURELIA_RELAY_KEY")
	if len(key) < 16 {
		log.Fatal("set AURELIA_RELAY_KEY to a secret of at least 16 characters, as from `openssl rand -hex 24`: hosts give it to the relay")
	}
	s := newServer(key, *maxRooms)
	s.trustProxy = *trustProxy
	srv := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
	if *domain == "" {
		log.Printf("the relay listens on %s", *addr)
		srv.Addr = *addr
		log.Fatal(srv.ListenAndServe())
	}
	m := &autocert.Manager{Prompt: autocert.AcceptTOS, HostPolicy: autocert.HostWhitelist(*domain), Cache: autocert.DirCache(*certs)}
	go func() {
		// The challenges, and to HTTPS for everything else.
		log.Fatal(http.ListenAndServe(":80", m.HTTPHandler(nil)))
	}()
	srv.Addr = ":443"
	// HTTP/1.1 alone: a host's connection is taken over from it.
	srv.TLSConfig = &tls.Config{GetCertificate: m.GetCertificate, NextProtos: []string{"http/1.1", "acme-tls/1"}}
	log.Printf("the relay listens on https://%s", *domain)
	log.Fatal(srv.ListenAndServeTLS("", ""))
}
