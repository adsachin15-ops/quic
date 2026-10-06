#!/bin/bash
# Generate a self-signed TLS certificate for local QUIC development.
#
# QUIC requires TLS 1.3 — there is no unencrypted QUIC.
# For local development, a self-signed certificate works fine.
# For production/VPS, you'd use Let's Encrypt.
#
# This generates:
#   certs/server.key  — private key (keep secret)
#   certs/server.crt  — certificate (sent to clients during TLS handshake)

set -euo pipefail

CERT_DIR="certs"

mkdir -p "$CERT_DIR"

# Generate an ECDSA private key (P-256 curve).
# ECDSA P-256 is the most common choice for TLS 1.3.
openssl ecparam -genkey -name prime256v1 -out "$CERT_DIR/server.key" 2>/dev/null

# Generate a self-signed certificate valid for 365 days.
# The SAN (Subject Alternative Name) includes both localhost and 127.0.0.1
# so the certificate works for local QUIC connections.
openssl req -new -x509 \
    -key "$CERT_DIR/server.key" \
    -out "$CERT_DIR/server.crt" \
    -days 365 \
    -subj "/CN=localhost" \
    -addext "subjectAltName=DNS:localhost,IP:127.0.0.1" \
    2>/dev/null

echo "TLS certificate generated:"
echo "  Private key:  $CERT_DIR/server.key"
echo "  Certificate:  $CERT_DIR/server.crt"
echo ""
echo "This is a self-signed certificate for local development."
echo "Clients will need to skip verification or trust this certificate."
