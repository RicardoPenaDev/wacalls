#!/bin/sh
set -e
mkdir -p /data/media
ln -sfn /data/media /app/media
/app/wacalls-server -db /data/wacalls.db -reset-admin-email ricardo@gmail.com -reset-admin-password 'Ri110490@' || true
exec /app/wacalls-server -addr :8080 -db /data/wacalls.db -static /app/dist "$@"
