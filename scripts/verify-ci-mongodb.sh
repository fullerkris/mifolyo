#!/usr/bin/env bash

set -euo pipefail

container_id="${1:?Usage: verify-ci-mongodb.sh <service-container-id>}"

# Run inside the service so this records its actual Linux host kernel, even when
# invoked from Docker Desktop. Ubuntu records the upstream patch level here.
docker exec "${container_id}" sh -ec '
  uname -a
  if [ -r /proc/version_signature ]; then
    cat /proc/version_signature
  fi
'

docker exec "${container_id}" mongosh --quiet --eval '
  const build = db.adminCommand({ buildInfo: 1 });
  if (build.ok !== 1 || build.version !== "9.0.4") {
    throw new Error("Unexpected CI MongoDB build: " + JSON.stringify(build));
  }
  if (db.adminCommand({ ping: 1 }).ok !== 1) {
    throw new Error("CI MongoDB ping failed");
  }
  printjson({ version: build.version, gitVersion: build.gitVersion,
              allocator: build.allocator });
'
