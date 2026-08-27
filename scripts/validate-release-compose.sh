#!/usr/bin/env bash

set -euo pipefail

static_validation=false
env_files=()
while (( $# > 0 )); do
  case "$1" in
    --static)
      static_validation=true
      shift
      ;;
    --env-file)
      if (( $# < 2 )); then
        echo "--env-file requires a path." >&2
        exit 1
      fi
      env_files+=("$2")
      shift 2
      ;;
    --*)
      echo "Unknown option '$1'." >&2
      exit 1
      ;;
    *)
      break
      ;;
  esac
done

release_tag="${1-${MIFOLYO_RELEASE_TAG-}}"
repository="fullerkris/mifolyo"
services=(spider indexer image-indexer page-rank)
image_variables=(
  MIFOLYO_SPIDER_IMAGE
  MIFOLYO_INDEXER_IMAGE
  MIFOLYO_IMAGE_INDEXER_IMAGE
  MIFOLYO_PAGE_RANK_IMAGE
)

if (( $# > 1 )); then
  echo "Usage: $0 [--static | --env-file PATH ...] RELEASE_TAG" >&2
  exit 1
fi

if [[ "${static_validation}" == true && ${#env_files[@]} -ne 0 ]]; then
  echo "--static and --env-file cannot be combined." >&2
  exit 1
fi

if [[ ! "${release_tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]] ||
  (( ${#release_tag} > 128 )); then
  echo "Invalid immutable release tag '${release_tag}'. Expected vMAJOR.MINOR.PATCH with an optional pre-release suffix; latest, main, branch names, build metadata, and empty tags are rejected." >&2
  exit 1
fi

export MIFOLYO_RELEASE_TAG="${release_tag}"

if (( ${#env_files[@]} > 0 )); then
  # In metadata mode, only the reviewed files may provide deployment images.
  # Ambient values must not conceal a missing or incomplete artifact.
  for image_variable in "${image_variables[@]}"; do
    unset "${image_variable}"
  done

  seen_image_variables=()
  for env_file in "${env_files[@]}"; do
    if [[ ! -r "${env_file}" ]]; then
      echo "Release image metadata is not readable: ${env_file}" >&2
      exit 1
    fi

    file_has_release_tag=false
    file_image_count=0
    while IFS= read -r line || [[ -n "${line}" ]]; do
      line="${line%$'\r'}"
      [[ -z "${line}" || "${line}" == \#* ]] && continue
      if [[ "${line}" != *=* ]]; then
        echo "${env_file}: malformed metadata line '${line}'." >&2
        exit 1
      fi
      key="${line%%=*}"
      value="${line#*=}"
      case "${key}" in
        MIFOLYO_RELEASE_TAG)
          if [[ "${file_has_release_tag}" == true ]]; then
            echo "${env_file}: duplicate MIFOLYO_RELEASE_TAG metadata." >&2
            exit 1
          fi
          if [[ "${value}" != "${release_tag}" ]]; then
            echo "${env_file}: release metadata '${value}' does not match '${release_tag}'." >&2
            exit 1
          fi
          file_has_release_tag=true
          ;;
        MIFOLYO_SPIDER_IMAGE|MIFOLYO_INDEXER_IMAGE|MIFOLYO_IMAGE_INDEXER_IMAGE|MIFOLYO_PAGE_RANK_IMAGE)
          if [[ " ${seen_image_variables[*]-} " == *" ${key} "* ]]; then
            echo "${env_file}: duplicate ${key} metadata." >&2
            exit 1
          fi
          seen_image_variables+=("${key}")
          printf -v "${key}" '%s' "${value}"
          export "${key}"
          (( file_image_count += 1 ))
          ;;
        *)
          echo "${env_file}: unexpected metadata key '${key}'." >&2
          exit 1
          ;;
      esac
    done < "${env_file}"

    if [[ "${file_has_release_tag}" != true || ${file_image_count} -ne 1 ]]; then
      echo "${env_file}: expected exactly one MIFOLYO_RELEASE_TAG and one service-specific image entry." >&2
      exit 1
    fi
  done

  if (( ${#seen_image_variables[@]} != ${#image_variables[@]} )); then
    echo "Release metadata must provide exactly one image entry for each approved deployment service." >&2
    for image_variable in "${image_variables[@]}"; do
      if [[ " ${seen_image_variables[*]-} " != *" ${image_variable} "* ]]; then
        echo "Missing metadata: ${image_variable}" >&2
      fi
    done
    exit 1
  fi
fi

dummy_digits=(1 2 3 4)

for index in "${!services[@]}"; do
  service="${services[$index]}"
  image_variable="${image_variables[$index]}"
  if [[ "${static_validation}" == true ]]; then
    printf -v digest '%064d' "${dummy_digits[$index]}"
    printf -v "${image_variable}" 'ghcr.io/%s/%s@sha256:%s' \
      "${repository}" "${service}" "${digest}"
    export "${image_variable}"
  elif [[ -z "${!image_variable-}" ]]; then
    echo "${image_variable} is required and must identify the approved ${service} image by digest." >&2
    exit 1
  fi
done

for index in "${!services[@]}"; do
  service="${services[$index]}"
  image_variable="${image_variables[$index]}"
  compose_file="services/${service}/docker-compose.yml"
  expected_image="${!image_variable}"

  if [[ ! "${expected_image}" =~ ^ghcr\.io/${repository}/${service}@sha256:[0-9a-f]{64}$ ]]; then
    echo "${image_variable} must exactly match ghcr.io/${repository}/${service}@sha256:<64 lowercase hex>; got '${expected_image}'." >&2
    exit 1
  fi

  if env -u "${image_variable}" docker compose --file "${compose_file}" \
    config --quiet >/dev/null 2>&1; then
    echo "${compose_file}: ${image_variable} must be required with no tag or default fallback." >&2
    exit 1
  fi

  resolved="$(docker compose --file "${compose_file}" config --format json)"

  python3 -c '
import json
import sys

compose_file, expected_image = sys.argv[1:]
document = json.load(sys.stdin)
services = document.get("services", {})
if len(services) != 1:
    raise SystemExit(f"{compose_file}: expected exactly one service, got {sorted(services)}")
name, service = next(iter(services.items()))
actual_image = service.get("image")
if actual_image != expected_image:
    raise SystemExit(
        f"{compose_file}: {name} resolved image {actual_image!r}, expected {expected_image!r}"
    )
if service.get("pull_policy") != "always":
    raise SystemExit(f"{compose_file}: {name} must set pull_policy: always")
if "build" in service:
    raise SystemExit(f"{compose_file}: {name} is a deployment definition and must not build locally")
' "${compose_file}" "${expected_image}" <<< "${resolved}"

  # Prove that the service image is actually bound to its required variable,
  # rather than to a digest hard-coded to match one static-validation value.
  if [[ "${expected_image}" == *@sha256:$(printf 'f%.0s' {1..64}) ]]; then
    alternate_digest="$(printf 'e%.0s' {1..64})"
  else
    alternate_digest="$(printf 'f%.0s' {1..64})"
  fi
  alternate_image="ghcr.io/${repository}/${service}@sha256:${alternate_digest}"
  alternate_resolved="$(env "${image_variable}=${alternate_image}" \
    docker compose --file "${compose_file}" config --format json)"
  python3 -c '
import json
import sys

compose_file, expected_image = sys.argv[1:]
service = next(iter(json.load(sys.stdin)["services"].values()))
if service.get("image") != expected_image:
    raise SystemExit(
        f"{compose_file}: image is not bound exclusively to its service-specific variable"
    )
' "${compose_file}" "${alternate_image}" <<< "${alternate_resolved}"

  printf 'validated %s -> %s (pull_policy=always, build=absent)\n' \
    "${compose_file}" "${expected_image}"
done
