#!/bin/sh
set -eu

: "${DOMAIN:?DOMAIN must be set (see .env) - required in both TLS_MODE values}"

# The choice between Let's Encrypt and a self-signed cert is a different
# Caddy directive entirely, not a value that fits inside one placeholder —
# so this picks the whole directive text before Caddy ever sees the file.
# Caddy's own {$VAR} substitution (not this script) fills in
# LETSENCRYPT_EMAIL/DOMAIN from the environment when it loads the result.
case "${TLS_MODE:-selfsigned}" in
  letsencrypt)
    : "${LETSENCRYPT_EMAIL:?LETSENCRYPT_EMAIL must be set when TLS_MODE=letsencrypt}"
    TLS_DIRECTIVE='tls {$LETSENCRYPT_EMAIL}'
    ;;
  selfsigned)
    TLS_DIRECTIVE="tls internal"
    ;;
  *)
    echo "caddy-entrypoint: TLS_MODE must be 'letsencrypt' or 'selfsigned', got '${TLS_MODE}'" >&2
    exit 1
    ;;
esac

sed "s|__TLS_DIRECTIVE__|${TLS_DIRECTIVE}|" /etc/caddy/Caddyfile.template > /etc/caddy/Caddyfile

exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile "$@"
