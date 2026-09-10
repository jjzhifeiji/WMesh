#!/bin/sh
set -eu
/usr/local/bin/wmesh &
exec nginx -g "daemon off;"
