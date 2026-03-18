#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tag="daily-office:latest"
extra_args=()

while (($# > 0)); do
  case "$1" in
    --tag)
      tag="$2"
      shift 2
      ;;
    *)
      extra_args+=("$1")
      shift
      ;;
  esac
done

docker build \
  --tag "${tag}" \
  --build-arg APT_MIRROR="https://mirrors.tuna.tsinghua.edu.cn/debian" \
  --build-arg APT_SECURITY_MIRROR="https://mirrors.tuna.tsinghua.edu.cn/debian-security" \
  --build-arg PIP_INDEX_URL="https://pypi.tuna.tsinghua.edu.cn/simple" \
  --build-arg NPM_REGISTRY="https://registry.npmmirror.com" \
  "${extra_args[@]}" \
  -f "${script_dir}/Dockerfile" \
  "${script_dir}"
