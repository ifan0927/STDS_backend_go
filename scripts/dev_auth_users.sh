#!/usr/bin/env sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname "$0")" && pwd)

PASSWORD="${DEV_AUTH_PASSWORD:-Test123!}"

"$SCRIPT_DIR/dev_auth_user.sh" \
  --uid local-admin \
  --email local-admin@example.com \
  --password "$PASSWORD" \
  --name "Local Admin" \
  --role admin

"$SCRIPT_DIR/dev_auth_user.sh" \
  --uid local-organizer \
  --email local-organizer@example.com \
  --password "$PASSWORD" \
  --name "Local Organizer" \
  --role organizer

"$SCRIPT_DIR/dev_auth_user.sh" \
  --uid local-staff \
  --email local-staff@example.com \
  --password "$PASSWORD" \
  --name "Local Staff" \
  --role staff

"$SCRIPT_DIR/dev_auth_user.sh" \
  --uid local-owner \
  --email local-owner@example.com \
  --password "$PASSWORD" \
  --name "Local Owner" \
  --role owner
