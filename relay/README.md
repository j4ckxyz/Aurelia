# Aurelia's Jam relay

A Jam works on its own between computers on the same network, or the same
tailnet. A relay lets one be joined from **any** network: the host
connects out to it, and guests reach the host through it. Nothing changes
for a Jam on one network: guests try the network first, and the relay only
when the host is not there.

Aurelia uses its public relay, `jam.j4ck.xyz`, unless the user turns it
off or sets one of their own. This is the program it runs, for anyone to
run their own.

It is small: a Go program of a few megabytes in memory. It keeps nothing
on disk, and nothing once a Jam ends. Its real cost is bandwidth: the
songs a guest plays from the host pass through it, about what streaming
them costs, once per guest who does not have the song on a server of
their own.

## How it works

- The host's Aurelia connects a WebSocket to `wss://your-relay/v2/host`
  and keeps that one connection open. The relay carries every guest's
  connection to the host over it, multiplexed with yamux.
- A guest connects a WebSocket to `wss://your-relay/v2/r/<room>`. This
  is a tunnel: the relay passes its bytes on to the host as they are.
- **Inside the tunnel, the guest and the host speak TLS.** Each Jam has a
  key of its own, made when it starts. The invite carries the key's
  fingerprint:
  `aurelia://jam/192.168.1.20:47475/K7QX4R?via=https://your-relay/v2/r/<room>#<fingerprint>`.
  The guest checks the host's key against it. The relay cannot read or
  change what goes through, and a relay that answered as the host would
  be found out before anything is said to it.
- A guest with only the code looks it up at `/v2/lookup`, which gives the
  room and the host's fingerprint as the host registered them. A Jam
  joined that way is only as private as the relay is trustworthy. An
  invite does not rely on the relay.
- Guests need no key. What lets them in is the same as on a local
  network: the Jam's code, or the host's yes.

The relay sees who connects, when, and how much. It never sees what is
said, nor what plays.

## Running it

### A relay of your own

It needs a key, any secret of 16 characters or more, which you give to
Aurelia too:

```sh
openssl rand -hex 24
```

### A public relay

Run it with `-open`, and anyone may host a Jam there without a key. A
key built into an open-source app would be no secret, so a public relay
relies on its limits instead (below). Behind Cloudflare, give it
`-ip-header CF-Connecting-IP`, so that the limits count each user by
their own address, not Cloudflare's. In that case, let only Cloudflare's
addresses reach the server, since anyone who can reach it directly could
send that header themselves.

### With Docker

```sh
docker build -t aurelia-relay .
docker run -d --name aurelia-relay --restart unless-stopped \
  -e AURELIA_RELAY_KEY=… -p 127.0.0.1:8787:8787 \
  --memory 192m aurelia-relay -addr :8787 -behind-proxy
```

For a public relay, leave out the key and add `-open`.

### Without Docker

Build it anywhere Go 1.25 or later is installed. It cross-compiles, and
the server needs nothing installed:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags='-s -w' -o aurelia-relay .
scp aurelia-relay you@server:/usr/local/bin/
```

Then run it as a service, with `aurelia-relay.service` from this folder:

```sh
sudo useradd --system --no-create-home aurelia-relay
sudo cp aurelia-relay.service /etc/systemd/system/
sudo systemctl edit aurelia-relay   # the key, or the flags of a public relay
sudo systemctl enable --now aurelia-relay
```

### HTTPS

Run it behind the reverse proxy you already have. With Caddy:

```
relay.example.com {
	reverse_proxy 127.0.0.1:8787
}
```

With nginx, which must pass the WebSocket upgrade on and not buffer:

```nginx
location / {
    proxy_pass http://127.0.0.1:8787;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;  # map $http_upgrade $connection_upgrade { default upgrade; '' close; }
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_buffering off;
    proxy_read_timeout 1h;
    proxy_send_timeout 1h;
}
```

Run it with `-behind-proxy` there, so that its limits count guests by
their own address and not the proxy's (or with `-ip-header`, as above).
Cloudflare in front passes WebSockets on as they are.

Or let it get its own certificate, with ports 80 and 443 free:

```sh
AURELIA_RELAY_KEY=… aurelia-relay -domain relay.example.com
```

### Then, in Aurelia

Settings ▸ Beta ▸ **Your own relay**: the relay's address
(`https://relay.example.com`), and its key if it has one. A Jam you host
then says "Reachable through your relay", and its invite works on any
network.

## Options

| Flag | |
|---|---|
| `-addr` | where to listen for HTTP (default `:8787`) |
| `-domain` | serve HTTPS itself, with a certificate from Let's Encrypt |
| `-certs` | where `-domain` keeps certificates (default `certs`) |
| `-open` | anyone may host, without the key: a public relay |
| `-behind-proxy` | trust `X-Forwarded-For` from the proxy in front |
| `-ip-header` | the header a client's address is in, as `CF-Connecting-IP` |
| `-max-rooms` | Jams carried at once (default 64) |
| `-rooms-per-address` | of them, from one address (default 3) |
| `-max-connections` | guests' connections carried at once (default 1024) |
| `-connections-per-jam` | of them, to one Jam (default 48) |
| `-mbps` | bandwidth at most, all told, in megabits a second (default 200; 0 for no cap) |
| `-jam-mbps` | bandwidth at most of one Jam (default 24; 0 for no cap) |

`GET /healthz` reports whether it is up, and how many Jams and
connections it is carrying.

## Limits

- Hosting: with the key, or by anyone on an `-open` relay. Tries are
  limited to 10 a minute per address, and each address may host 3 Jams at
  a time. A room stays its host's own, by a secret only the host has. A
  Jam's code cannot be taken by another Jam.
- Guests: anyone with an invite reaches the host, which lets in only
  those with the code, or those it says yes to. The host slows down
  wrong codes itself. Looking up a code is limited to 30 tries a minute
  per address, and 1,200 a minute in all. Each address may open 240
  connections a minute.
- What a Jam may use is capped too: connections, and bandwidth. A
  connection that carries nothing for five minutes is closed.
- A browser cannot use the relay. A WebSocket from a web page (one that
  sends an `Origin` header) is refused, and nothing the relay serves is a
  page.
- A Jam's room lasts as long as its host's connection. The host
  reconnects by itself, to the same room, and its invites stay good.
- IPv6 addresses are counted by their /64, which one computer may have
  all of.
