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
// Hosts need the key, unless it is run -open, for anyone to host; guests
// never do: the Jam's code, or its host's yes, lets them in, as on the
// local network. What it carries is encrypted between the host and its
// guests: the relay passes it on, and cannot read it.
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
	lim := defaultLimits()
	open := flag.Bool("open", false, "let anyone host a Jam here, without the key: a public relay")
	flag.IntVar(&lim.rooms, "max-rooms", lim.rooms, "how many Jams the relay carries at once")
	flag.IntVar(&lim.roomsPerClient, "rooms-per-address", lim.roomsPerClient, "how many of them one address may host")
	flag.IntVar(&lim.tunnels, "max-connections", lim.tunnels, "how many guests' connections it carries at once")
	flag.IntVar(&lim.tunnelsPerRoom, "connections-per-jam", lim.tunnelsPerRoom, "how many of them one Jam may have")
	mbps := flag.Float64("mbps", lim.rate*8/1e6, "the bandwidth it uses at most, all told, in megabits a second; 0 for no cap")
	roomMbps := flag.Float64("jam-mbps", lim.roomRate*8/1e6, "the bandwidth one Jam uses at most, in megabits a second; 0 for no cap")
	trustProxy := flag.Bool("behind-proxy", false, "a reverse proxy is in front: a client's address is the last of X-Forwarded-For")
	ipHeader := flag.String("ip-header", "", "the header a client's address is in, from the proxy in front, as CF-Connecting-IP behind Cloudflare")
	flag.Parse()
	lim.rate, lim.roomRate = *mbps*1e6/8, *roomMbps*1e6/8

	key := os.Getenv("AURELIA_RELAY_KEY")
	if !*open && len(key) < 16 {
		log.Fatal("set AURELIA_RELAY_KEY to a secret of at least 16 characters, as from `openssl rand -hex 24`, which hosts give the relay; or run it -open, for anyone to host")
	}
	s := newServer(key, *open, lim)
	s.xff, s.ipHeader = *trustProxy, *ipHeader
	srv := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
	if *domain == "" {
		if *open {
			log.Printf("anyone may host a Jam here")
		}
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
