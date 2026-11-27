# Aurelia's Jam relay

A Jam works on its own between computers on the same network, or the same
tailnet. This relay, run on a server of yours, lets one be joined from
**any** network: the host connects out to the relay, and guests reach the
host through it. Nothing changes for a Jam on one network: guests try the
network first, and the relay only when the host is not there.

It is small. A Go program of a few megabytes in memory, it keeps nothing on
disk and nothing once a Jam ends; its only real cost is the bandwidth of
the songs a guest plays from the host (about what streaming them costs,
once per guest who has not got the song from a server of their own).

## How it works

- The host's Aurelia connects to `https://your-relay/v1/host` with the
  relay's **key**, and keeps that one connection open. The relay carries
  every guest's connections to the host over it (multiplexed with yamux).
- A host's invite names the relay too:
  `aurelia://jam/192.168.1.20:47475/K7QX4R?via=https://your-relay/r/…`. A
  guest tries the address on its own network first, then the relay.
- A guest with only the code looks it up on the relay set in its own
  settings, if the code is not found on its network.
- Guests need no key. What lets them in is what does on a network: the
  Jam's code, or the host's yes. The relay itself sees the Jam's traffic
  (it is yours); between it and everyone, it is HTTPS.

## Running it

It needs a key, any secret of 16 characters or more, which you give to
Aurelia too:

```sh
openssl rand -hex 24
```

### With Docker

```sh
docker build -t aurelia-relay .
docker run -d --name aurelia-relay --restart unless-stopped \
  -e AURELIA_RELAY_KEY=… -p 127.0.0.1:8787:8787 \
  --memory 64m aurelia-relay -addr :8787 -behind-proxy
```

### Without Docker

Build it anywhere Go 1.25 or later is (it cross-compiles; the server needs
nothing installed):

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags='-s -w' -o aurelia-relay .
scp aurelia-relay you@server:/usr/local/bin/
```

then run it as a service, with `aurelia-relay.service` here:

```sh
sudo useradd --system --no-create-home aurelia-relay
sudo cp aurelia-relay.service /etc/systemd/system/
sudo systemctl edit aurelia-relay   # [Service] Environment=AURELIA_RELAY_KEY=…
sudo systemctl enable --now aurelia-relay
```

### HTTPS

Behind the reverse proxy you have, as Caddy:

```
relay.example.com {
	reverse_proxy 127.0.0.1:8787
}
```

or nginx, which must pass the upgrade on, and not buffer:

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
their own address and not the proxy's.

Or let it get its own certificate, with ports 80 and 443 free:

```sh
AURELIA_RELAY_KEY=… aurelia-relay -domain relay.example.com
```

### Then, in Aurelia

Settings ▸ Beta ▸ **Jam relay**: the relay's address
(`https://relay.example.com`) and its key. A Jam you host then says
"Reachable through your relay", and its invite works on any network.

## Options

| Flag | |
|---|---|
| `-addr` | where to listen for HTTP (default `:8787`) |
| `-domain` | serve HTTPS itself, with a certificate from Let's Encrypt |
| `-certs` | where `-domain` keeps certificates (default `certs`) |
| `-max-rooms` | Jams carried at once (default 32) |
| `-behind-proxy` | trust `X-Forwarded-For` from the proxy in front |

`GET /healthz` says it is up, and how many Jams it carries.

## Limits

- Hosts: those with the key. Trying keys is slowed (30 tries a minute per
  address).
- Guests: anyone with an invite reaches the host, which lets in only those
  with the code or whom it says yes to. Looking a code up is slowed to 30
  tries a minute per address.
- A Jam's room lives as long as its host's connection: the host reconnects
  by itself, to the same room, and its invites stay good.
