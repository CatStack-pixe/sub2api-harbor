#!/bin/sh
set -eu

# Debian/Mozilla distrust for the October 2026 CA incident. Remove the source
# certificates as well as refreshing generated symlinks/bundles, so a later
# update-ca-certificates cannot silently re-enable these roots.
for name in Comodo_AAA_Services_root emSign_ECC_Root_CA_-_G3; do
    rm -f "/usr/share/ca-certificates/mozilla/${name}.crt"
    if [ -f /etc/ca-certificates.conf ]; then
        sed -i "\|^!\\{0,1\\}mozilla/${name}\\.crt\$|d" /etc/ca-certificates.conf
    fi
done
update-ca-certificates --fresh
