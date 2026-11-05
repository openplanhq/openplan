set -eu
# The CLIs a session draws diagrams with: draw.io's (the drawio skill converts
# Mermaid and exports with it) and Mermaid's own. Run as root, once per VM;
# a re-run reinstalls the same pins.

# sessions/ can be installed straight after vm/, while cloud-init still holds
# apt's lock. How the setup ended doesn't matter here.
cloud-init status --wait > /dev/null || true

export DEBIAN_FRONTEND=noninteractive
apt-get update -q
# xvfb and xauth for xvfb-run. Debian's Chromium brings the libraries a
# headless browser needs; Puppeteer's own download would leave them to us.
apt-get install -y -q --no-install-recommends xvfb xauth chromium nodejs npm

# draw.io's CLI is its desktop app, which Debian doesn't package: pinned, and
# checked before it's installed.
deb="$(mktemp --suffix=.deb)"
curl -fsSL --retry 3 -o "$deb" https://github.com/jgraph/drawio-desktop/releases/download/v32.6.1/drawio-amd64-32.6.1.deb
echo "515a975fbdb982e0d736b6f99248dbbb856a8b07bb72d6d3648a54b850fa33db  $deb" | sha256sum -c -
apt-get install -y -q --no-install-recommends "$deb"
rm -f "$deb"

# 11, not 12: 12 needs Node 22, and Debian 13 ships 20.
PUPPETEER_SKIP_DOWNLOAD=1 npm install -g -q --prefix /opt/mermaid-cli @mermaid-js/mermaid-cli@11.17.1

# Ahead of /usr/bin on PATH, so a session calls these. The app is Electron and
# needs a display: xvfb-run gives it one, -a a free one per call so parallel
# sessions don't collide, and a VM has no GPU.
cat > /usr/local/bin/drawio <<'EOF'
#!/bin/sh
exec xvfb-run -a /opt/drawio/drawio --disable-gpu "$@"
EOF
cat > /usr/local/bin/mmdc <<'EOF'
#!/bin/sh
PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium exec /opt/mermaid-cli/bin/mmdc "$@"
EOF
chmod 0755 /usr/local/bin/drawio /usr/local/bin/mmdc
