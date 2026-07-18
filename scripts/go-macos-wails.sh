#!/usr/bin/env bash

set -euo pipefail

readonly framework_flag="-framework UniformTypeIdentifiers"

if [[ " ${CGO_LDFLAGS:-} " != *" ${framework_flag} "* ]]; then
  export CGO_LDFLAGS="${CGO_LDFLAGS:+${CGO_LDFLAGS} }${framework_flag}"
fi

exec go "$@"
