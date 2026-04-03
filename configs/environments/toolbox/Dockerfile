ARG PYTHON_BASE_IMAGE=python:3.12-slim-bookworm
FROM ${PYTHON_BASE_IMAGE}

ARG APT_MIRROR=""
ARG APT_SECURITY_MIRROR=""
ARG APT_RETRIES=5
ARG APT_TIMEOUT=30
ARG DBX_VERSION=0.1.0
ARG HTTPX_VERSION=0.1.0
ARG MOCK_VERSION=0.1.0
ARG DBX_DOWNLOAD_URL=""
ARG HTTPX_DOWNLOAD_URL=""
ARG MOCK_DOWNLOAD_URL=""

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    TOOLBOX_HOME=/opt/toolbox \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

RUN set -euo pipefail; \
    apt_mirror="${APT_MIRROR}"; \
    apt_security_mirror="${APT_SECURITY_MIRROR:-${APT_MIRROR}}"; \
    if [[ -z "${apt_mirror}" ]]; then \
        apt_mirror="https://deb.debian.org/debian"; \
    fi; \
    if [[ -z "${apt_security_mirror}" ]]; then \
        apt_security_mirror="https://security.debian.org/debian-security"; \
    fi; \
    apt_mirror="${apt_mirror%/}"; \
    apt_security_mirror="${apt_security_mirror%/}"; \
    if [[ -f /etc/apt/sources.list ]]; then \
        sed -i \
            -e "s|http://deb.debian.org/debian|${apt_mirror}|g" \
            -e "s|https://deb.debian.org/debian|${apt_mirror}|g" \
            -e "s|http://security.debian.org/debian-security|${apt_security_mirror}|g" \
            -e "s|https://security.debian.org/debian-security|${apt_security_mirror}|g" \
            -e "s|http://deb.debian.org/debian-security|${apt_security_mirror}|g" \
            -e "s|https://deb.debian.org/debian-security|${apt_security_mirror}|g" \
            /etc/apt/sources.list; \
    fi; \
    if compgen -G "/etc/apt/sources.list.d/*.sources" > /dev/null; then \
        while IFS= read -r source_file; do \
            sed -i \
                -e "s|http://deb.debian.org/debian|${apt_mirror}|g" \
                -e "s|https://deb.debian.org/debian|${apt_mirror}|g" \
                -e "s|http://security.debian.org/debian-security|${apt_security_mirror}|g" \
                -e "s|https://security.debian.org/debian-security|${apt_security_mirror}|g" \
                -e "s|http://deb.debian.org/debian-security|${apt_security_mirror}|g" \
                -e "s|https://deb.debian.org/debian-security|${apt_security_mirror}|g" \
                "${source_file}"; \
        done < <(find /etc/apt/sources.list.d -maxdepth 1 -type f -name '*.sources' | sort); \
    fi; \
    apt-get \
        -o Acquire::Retries="${APT_RETRIES}" \
        -o Acquire::http::Timeout="${APT_TIMEOUT}" \
        -o Acquire::https::Timeout="${APT_TIMEOUT}" \
        update \
    && apt-get \
        -o Acquire::Retries="${APT_RETRIES}" \
        -o Acquire::http::Timeout="${APT_TIMEOUT}" \
        -o Acquire::https::Timeout="${APT_TIMEOUT}" \
        install -y --no-install-recommends \
        bash \
        ca-certificates \
        coreutils \
        curl \
        jq \
        sed \
        tar \
    && rm -rf /var/lib/apt/lists/*

RUN set -euo pipefail; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
        amd64) \
            dbx_asset="dbx_v${DBX_VERSION}_linux_amd64.tar.gz"; \
            httpx_asset="httpx_v${HTTPX_VERSION}_linux_amd64.tar.gz"; \
            mock_asset="mock_v${MOCK_VERSION}_linux_amd64.tar.gz"; \
            ;; \
        arm64) \
            dbx_asset="dbx_v${DBX_VERSION}_linux_arm64.tar.gz"; \
            httpx_asset="httpx_v${HTTPX_VERSION}_linux_arm64.tar.gz"; \
            mock_asset="mock_v${MOCK_VERSION}_linux_arm64.tar.gz"; \
            ;; \
        *) echo "Unsupported architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    install_cli() { \
        local cli_name="$1"; \
        local asset_url="$2"; \
        local tmpdir archive bin_path; \
        tmpdir="$(mktemp -d)"; \
        archive="${tmpdir}/$(basename "${asset_url%%\?*}")"; \
        curl -fsSL "${asset_url}" -o "${archive}"; \
        mkdir -p "${tmpdir}/unpacked"; \
        CLI_NAME="${cli_name}" ARCHIVE_PATH="${archive}" EXTRACT_DIR="${tmpdir}/unpacked" python -c 'import os, pathlib, shutil, tarfile, zipfile; archive = pathlib.Path(os.environ["ARCHIVE_PATH"]); extract_dir = pathlib.Path(os.environ["EXTRACT_DIR"]); cli_name = os.environ["CLI_NAME"]; name = archive.name.lower(); handler = "zip" if name.endswith(".zip") else "tar" if name.endswith((".tgz", ".tar.gz")) else None; assert handler, f"Unsupported archive format for {cli_name}: {archive}"; zf = zipfile.ZipFile(archive) if handler == "zip" else None; tf = tarfile.open(archive) if handler == "tar" else None; zf.extractall(extract_dir) if zf else None; tf.extractall(extract_dir, filter="fully_trusted") if tf else None; zf.close() if zf else None; tf.close() if tf else None; matches = [path for path in extract_dir.rglob(cli_name) if path.is_file() and os.access(path, os.X_OK)]; assert matches, f"Failed to locate the {cli_name} binary in {archive}"; shutil.copy2(matches[0], extract_dir / ".selected-binary")'; \
        bin_path="${tmpdir}/unpacked/.selected-binary"; \
        if [[ -z "${bin_path}" ]]; then \
            echo "Failed to locate the ${cli_name} binary in ${archive}" >&2; \
            exit 1; \
        fi; \
        install -m 0755 "${bin_path}" "/usr/local/bin/${cli_name}"; \
        rm -rf "${tmpdir}"; \
    }; \
    dbx_url="${DBX_DOWNLOAD_URL:-https://cligrep.com/cli-releases/dbx/v${DBX_VERSION}/${dbx_asset}}"; \
    httpx_url="${HTTPX_DOWNLOAD_URL:-https://cligrep.com/cli-releases/httpx/v${HTTPX_VERSION}/${httpx_asset}}"; \
    mock_url="${MOCK_DOWNLOAD_URL:-https://cligrep.com/cli-releases/mock/v${MOCK_VERSION}/${mock_asset}}"; \
    install_cli "dbx" "${dbx_url}"; \
    install_cli "httpx" "${httpx_url}"; \
    install_cli "mock" "${mock_url}"

RUN mkdir -p /workspace

WORKDIR /workspace
