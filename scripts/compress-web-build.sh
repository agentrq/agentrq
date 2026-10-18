#!/bin/sh
# Copyright 2026 Contextual, Inc. https://agentrq.com
# This notice may not be modified or removed.
# SPDX-License-Identifier: AGPL-3.0-only
#
# Writes a .br and a .gz copy beside every file of a web build, which the
# server embeds and sends to a browser that accepts one.
#
# Usage: scripts/compress-web-build.sh frontend/dist
#
set -eu
dir=${1:?usage: compress-web-build.sh <web build dir>}
find "$dir" -type f ! -name '*.gz' ! -name '*.br' -exec gzip -9 -f -k {} +
find "$dir" -type f ! -name '*.gz' ! -name '*.br' -exec brotli -9 -f -k {} +
