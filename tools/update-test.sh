#!/usr/bin/env bash
# Checks that Aurelia updates itself, on the machine this runs on: builds
# version 0.0.1, installs it as a user would, publishes 0.0.2 as an update
# on this machine, and has the installed app replace itself with it.
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
server=""

cp mygo.json "$work/mygo.json"
cleanup() {
  cp "$work/mygo.json" mygo.json
  [ -n "$server" ] && kill "$server" 2>/dev/null || true
  rm -rf "$out"
}
trap cleanup EXIT

go tool mygo keygen -o "$work/keys" > /dev/null
MYGO_UPDATER_PRIVATE_KEY=$(cat "$work/keys/mygo-update.key")
export MYGO_UPDATER_PRIVATE_KEY
printf '# Changes\n\n## 0.0.2\n\nThe update of the test.\n\n## 0.0.1\n\nThe first version of the test.\n' > "$work/CHANGELOG.md"

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
    "changelog": "$work/CHANGELOG.md"
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

"$app" --write-version "$work/before.txt"
echo "before: $(cat "$work/before.txt")"
grep -qx "0.0.1" "$work/before.txt"

echo "== Building 0.0.2, and publishing it on this machine"
rm -rf "$out"
build 0.0.2
# What the list of the update names is fetched over http too.
"$python" - "$dir" "$port" <<'PY'
import glob, sys
for path in glob.glob(sys.argv[1] + "/update-*.json"):
    text = open(path).read()
    open(path, "w").write(text.replace("https://127.0.0.1:%s/" % sys.argv[2], "http://127.0.0.1:%s/" % sys.argv[2]))
PY
"$python" -m http.server "$port" --bind 127.0.0.1 --directory "$dir" > "$work/server.log" 2>&1 &
server=$!
sleep 1
ls -la "$dir"

echo "== Updating"
"$app" --self-update "$work/update.txt"
cat "$work/update.txt"
grep -q "^updated: 0.0.1 -> 0.0.2" "$work/update.txt"

# The file at the same place is now the new version.
"$app" --write-version "$work/after.txt"
echo "after: $(cat "$work/after.txt")"
grep -qx "0.0.2" "$work/after.txt"

case "$os" in
  linux) sh "$dir/install.sh" --uninstall || true ;;
  windows) [ -f "$LOCALAPPDATA/Programs/AureliaUpdateTest/Uninstall.exe" ] && "$LOCALAPPDATA/Programs/AureliaUpdateTest/Uninstall.exe" //S || true ;;
esac
echo "== Aurelia updated itself from 0.0.1 to 0.0.2 on $os/$arch"
