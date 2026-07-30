#!/bin/sh
set -eu

if [ -z "${YYB_WEB_USER:-}" ] || [ -z "${YYB_WEB_PASSWORD:-}" ]; then
  echo "YYB_WEB_USER and YYB_WEB_PASSWORD are required" >&2
  exit 1
fi

htpasswd -bcB /etc/nginx/auth/htpasswd "$YYB_WEB_USER" "$YYB_WEB_PASSWORD"
chown root:nginx /etc/nginx/auth/htpasswd
chmod 640 /etc/nginx/auth/htpasswd
