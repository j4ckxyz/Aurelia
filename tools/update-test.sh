#!/usr/bin/env bash
# Checks that Aurelia updates itself, on the machine this runs on: builds
# version 0.0.1, installs it as a user would, publishes 0.0.2 as an update
# on this machine, and has the installed app replace itself with it.
#
# Then the same as a user meets it: 0.0.3 is published, the installed app
# is opened signed in, offers the new version on its page, is told to
# update, and must come back by itself as 0.0.3, still signed in. That
# part needs a display, as every desktop has (xvfb-run on a server).
#
#   tools/update-test.sh
#
# The build tool only takes https addresses for updates, and the app
# takes http from this machine alone: so the builds name
# https://127.0.0.1, and the test changes that to http in the app it
# installs and in the list of the update it publishes. Everything else is
# as released: the check, the download, its signature, the replacing.
#
# It signs with a key made for the test, changes mygo.json for the builds
# and puts it back, and installs nothing outside a temporary directory,
# except on Linux and Windows, where it installs the test's app as their
# installers do (and removes it).
set -euo pipefail
cd "$(dirname "$0")/.."

port=${PORT:-8765}
python=python3
command -v python3 > /dev/null 2>&1 || python=python
os=$(go env GOOS)
arch=$(go env GOARCH)
work=$(mktemp -d)
out=build-update-test
# In the project, so that its path reads the same to every program on
# Windows.
notes=build-update-test-changes.md
server=""

cp mygo.json "$work/mygo.json"
cleanup() {
  cp "$work/mygo.json" mygo.json
  [ -n "$server" ] && kill "$server" 2>/dev/null || true
  rm -rf "$out" "$notes"
}
trap cleanup EXIT

go tool mygo keygen -o "$work/keys" > /dev/null
MYGO_UPDATER_PRIVATE_KEY=$(cat "$work/keys/mygo-update.key")
export MYGO_UPDATER_PRIVATE_KEY
printf '# Changes\n\n## 0.0.3\n\n- The update the app offers as it opens.\n- Its second point.\n\n## 0.0.2\n\nThe update of the test.\n\n## 0.0.1\n\nThe first version of the test.\n' > "$notes"

# A name of its own, so that the test's app is apart from an installed
# Aurelia.
build() {
  cat > mygo.json <<JSON
{
  "name": "AureliaUpdateTest",
  "identifier": "dev.aurelia.updatetest",
  "version": "$1",
  "out": "$out",
  "linux": { "command": "aurelia-update-test" },
  "updates": {
    "publicKey": "$(cat "$work/keys/mygo-update.pub")",
    "url": "https://127.0.0.1:$port",
    "changelog": "$notes"
  }
}
JSON
  go tool mygo build -platform "$os/$arch"
}

echo "== Building 0.0.1"
build 0.0.1
dir="$out/$os-$arch"
ls -la "$dir"

echo "== Installing 0.0.1"
case "$os" in
  darwin)
    mkdir -p "$work/Applications"
    cp -R "$dir/AureliaUpdateTest.app" "$work/Applications/"
    app="$work/Applications/AureliaUpdateTest.app/Contents/MacOS/AureliaUpdateTest"
    ;;
  linux)
    # As install.sh installs it: in ~/.local, where the app can write.
    sh "$dir/install.sh" "$dir"/*-0.0.1-linux-"$arch".tar.gz
    app=$(find "$HOME/.local" -maxdepth 3 -type f -perm -u+x -iname 'aureliaupdatetest*' | head -1)
    ;;
  windows)
    installer=$(ls "$dir"/*[Ss]etup*.exe 2>/dev/null | head -1 || true)
    if [ -n "$installer" ]; then
      # The installer, silently: per user, in %LOCALAPPDATA%\Programs.
      "$installer" //S
      app="$LOCALAPPDATA/Programs/AureliaUpdateTest/AureliaUpdateTest.exe"
      for _ in 1 2 3 4 5 6 7 8 9 10; do [ -f "$app" ] && break; sleep 1; done
    else
      mkdir -p "$work/install"
      cp -R "$dir"/. "$work/install/"
      app="$work/install/AureliaUpdateTest.exe"
    fi
    ;;
esac
[ -f "$app" ] || { echo "the app was not installed: $app"; exit 1; }
echo "installed at $app"

# The address of updates, from https to http: the same length, so that
# the program stays whole.
plain_http() {
  "$python" - "$app" "$port" <<'PY'
import sys
path, port = sys.argv[1], sys.argv[2]
data = open(path, "rb").read()
old = ("https://127.0.0.1:%s/update-" % port).encode()
new = ("http://127.0.0.1:%s//update-" % port).encode()
assert len(old) == len(new) and data.count(old) >= 1, "the app does not name the test's address"
open(path, "wb").write(data.replace(old, new))
PY
  if [ "$os" = darwin ]; then
    # A program changed must be signed again to run.
    codesign --force --sign - "${app%/Contents/MacOS/*}"
  fi
}
plain_http

# publish serves the build in $dir as the update, its list naming http.
publish() {
  [ -n "$server" ] && kill "$server" 2>/dev/null || true
  "$python" - "$dir" "$port" <<'PY'
import glob, sys
for path in glob.glob(sys.argv[1] + "/update-*.json"):
    text = open(path).read()
    open(path, "w").write(text.replace("https://127.0.0.1:%s/" % sys.argv[2], "http://127.0.0.1:%s/" % sys.argv[2]))
PY
  "$python" -m http.server "$port" --bind 127.0.0.1 --directory "$dir" > "$work/server.log" 2>&1 &
  server=$!
  feed="http://127.0.0.1:$port/update-$os-$arch.json"
  for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
    curl -fsS -m 5 "$feed" > /dev/null 2>&1 && break
    sleep 1
  done
  curl -fsS -m 5 "$feed" || { echo "the update is not served at $feed"; cat "$work/server.log"; exit 1; }
  echo
}

"$app" --write-version "$work/before.txt"
echo "before: $(cat "$work/before.txt")"
grep -qx "0.0.1" "$work/before.txt"

echo "== Building 0.0.2, and publishing it on this machine"
rm -rf "$out"
build 0.0.2
publish

echo "== Updating"
"$app" --self-update "$work/update.txt" || true
cat "$work/update.txt"
if ! grep -q "^updated: 0.0.1 -> 0.0.2" "$work/update.txt"; then
  echo "-- what the server saw:"
  cat "$work/server.log"
  exit 1
fi

# The file at the same place is now the new version.
"$app" --write-version "$work/after.txt"
echo "after: $(cat "$work/after.txt")"
grep -qx "0.0.2" "$work/after.txt"

echo "== Aurelia updated itself from 0.0.1 to 0.0.2 on $os/$arch"

echo "== Building 0.0.3, and publishing it"
plain_http # the 0.0.2 that is installed now names https again
rm -rf "$out"
build 0.0.3
publish

echo "== Opening 0.0.2, signed in: it offers 0.0.3, updates and reopens"
"$app" --write-data-dir "$work/data-dir.txt"
data=$(cat "$work/data-dir.txt")
case "$data" in *AureliaUpdateTest* | *aureliaupdatetest* | *dev.aurelia.updatetest*) ;; *) echo "the test's app keeps its settings in $data, which is not its own"; exit 1 ;; esac
mkdir -p "$data"
cat > "$data/settings.json" <<JSON
{
  "session": {"server": "http://127.0.0.1:9", "serverId": "srv", "serverName": "Test", "userId": "u", "userName": "ada", "token": "t", "deviceId": "d", "deviceName": "test"},
  "deviceId": "d", "theme": "auto", "volume": 1, "normalize": true, "audioCacheMB": 300, "pictureCacheMB": 150, "autoUpdate": true
}
JSON
dbg="$work/dbg"
mkdir -p "$dbg"
# drive has the app that runs do something, and waits for it.
drive() {
  rm -f "$dbg/done"
  printf '%s\n' "$1" > "$dbg/do.tmp" && mv "$dbg/do.tmp" "$dbg/do"
  for _ in $(seq 1 "${2:-50}"); do
    [ -f "$dbg/done" ] && return 0
    sleep 0.2
  done
  rm -f "$dbg/do"
  return 1
}
state() { cat "$dbg/state" 2>/dev/null || true; }
AURELIA_DEBUG="$dbg" "$app" > "$work/app.log" 2>&1 &
offered=""
for _ in $(seq 1 60); do
  if drive state 10 && state | grep -q 'offered="0.0.3"'; then offered=yes; break; fi
  sleep 1
done
if [ -z "$offered" ]; then
  echo "the app did not offer 0.0.3:"; state; echo "-- its log:"; cat "$work/app.log"; echo "-- the server's:"; cat "$work/server.log"
  drive quit 10 || true
  exit 1
fi
state | grep '^version='
state | grep -q '^version=0.0.2 .*signedIn=true' || { echo "the app that offers the update is not 0.0.2, signed in"; exit 1; }
drive "update accept" 25 || true
reopened=""
for _ in $(seq 1 90); do
  sleep 1
  rm -f "$dbg/state"
  if drive state 10 && state | grep -q '^version=0.0.3 '; then reopened=yes; break; fi
done
if [ -z "$reopened" ]; then
  echo "the app did not come back as 0.0.3:"; state; echo "-- its log:"; cat "$work/app.log"; echo "-- the server's:"; cat "$work/server.log"
  drive quit 10 || true
  exit 1
fi
state | grep '^version='
state | grep -q 'signedIn=true' || { echo "the app that came back is signed out"; drive quit 10 || true; exit 1; }
drive quit 15 || true
sleep 1
rm -rf "$data"
echo "== Aurelia offered 0.0.3, updated and reopened by itself, signed in, on $os/$arch"

case "$os" in
  linux) sh "$dir/install.sh" --uninstall || true ;;
  windows) [ -f "$LOCALAPPDATA/Programs/AureliaUpdateTest/Uninstall.exe" ] && "$LOCALAPPDATA/Programs/AureliaUpdateTest/Uninstall.exe" //S || true ;;
esac
