#!/bin/sh
set -e
mkdir -p /data/media
ln -sfn /data/media /app/media
/app/wacalls-server -db /data/wacalls.db -reset-admin-email admin@admin.com -reset-admin-password 'admin' || true
exec /app/wacalls-server -addr :8080 -db /data/wacalls.db -static /app/dist "$@"
