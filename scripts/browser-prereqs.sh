#!/usr/bin/env bash
# Test-only prerequisites. Missing tools are failures, never skipped acceptance.
set -euo pipefail
for tool in node go rg; do
    command -v "$tool" >/dev/null 2>&1 || {
        echo "browser-check: $tool is required (Node 24, Go and ripgrep)" >&2
        exit 1
    }
done
node -e 'if (Number(process.versions.node.split(".")[0]) !== 24) {
    console.error("browser-check: Node 24 is required; found " + process.version);
    process.exit(1);
}'
chrome="${CHROME_BIN:-/bin/google-chrome}"
command -v "$chrome" >/dev/null 2>&1 || {
    echo 'browser-check: Chrome/Chromium is required; set CHROME_BIN to its executable' >&2
    exit 1
}
node --version
"$chrome" --version
