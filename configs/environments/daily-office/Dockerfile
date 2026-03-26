ARG PYTHON_BASE_IMAGE=python:3.12-slim-bookworm
FROM ${PYTHON_BASE_IMAGE}

ARG APT_MIRROR=""
ARG APT_SECURITY_MIRROR=""
ARG APT_RETRIES=5
ARG APT_TIMEOUT=30
ARG HIMALAYA_VERSION=1.2.0
ARG DBX_VERSION=0.1.0
ARG HTTPX_VERSION=0.1.0
ARG HIMALAYA_DOWNLOAD_URL=""
ARG DBX_DOWNLOAD_URL=""
ARG HTTPX_DOWNLOAD_URL=""
ARG PIP_INDEX_URL=""
ARG PIP_EXTRA_INDEX_URL=""
ARG NPM_REGISTRY=""

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    NPM_CONFIG_UPDATE_NOTIFIER=false \
    DAILY_OFFICE_HOME=/opt/daily-office \
    NODE_PATH=/opt/daily-office/node_modules \
    PATH=/opt/daily-office/node_modules/.bin:/skills/scripts:/skills/docx/scripts:/skills/pptx/scripts:/skills/pdf/scripts:/skills/xlsx/scripts:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

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
        bzip2 \
        ca-certificates \
        coreutils \
        curl \
        dnsutils \
        file \
        findutils \
        fonts-dejavu-core \
        fonts-liberation \
        fonts-noto-cjk \
        fonts-noto-color-emoji \
        gawk \
        gcc \
        g++ \
        git \
        grep \
        gzip \
        iputils-ping \
        jq \
        less \
        libreoffice \
        make \
        nano \
        netcat-openbsd \
        nodejs \
        npm \
        pandoc \
        poppler-utils \
        procps \
        psmisc \
        qpdf \
        rsync \
        sed \
        tar \
        tesseract-ocr \
        tree \
        unzip \
        vim-tiny \
        wget \
        which \
        xz-utils \
        zip \
    && rm -rf /var/lib/apt/lists/*

RUN set -euo pipefail; \
    pip_args=(); \
    if [[ -n "${PIP_INDEX_URL}" ]]; then \
        pip_args+=(--index-url "${PIP_INDEX_URL}"); \
    fi; \
    if [[ -n "${PIP_EXTRA_INDEX_URL}" ]]; then \
        pip_args+=(--extra-index-url "${PIP_EXTRA_INDEX_URL}"); \
    fi; \
    python -m pip install --upgrade "${pip_args[@]}" pip \
    && python -m pip install "${pip_args[@]}" \
        defusedxml \
        lxml \
        'markitdown[pptx]' \
        openpyxl \
        pandas \
        pdf2image \
        pdfplumber \
        Pillow \
        pypdf \
        pypdfium2 \
        pytesseract \
        reportlab

RUN set -euo pipefail; \
    mkdir -p "${DAILY_OFFICE_HOME}"; \
    npm_registry="${NPM_REGISTRY:-https://registry.npmjs.org}"; \
    npm install --prefix "${DAILY_OFFICE_HOME}" --omit=dev --no-package-lock --registry "${npm_registry}" \
        docx@^9.1.1 \
        pptxgenjs@^3.12.0 \
        react@^18.3.1 \
        react-dom@^18.3.1 \
        react-icons@^5.4.0 \
        sharp@^0.33.5

RUN set -euo pipefail; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
        amd64) \
            himalaya_asset="himalaya.x86_64-linux.tgz"; \
            dbx_asset="dbx_v${DBX_VERSION}_linux_amd64.tar.gz"; \
            httpx_asset="httpx_v${HTTPX_VERSION}_linux_amd64.tar.gz"; \
            ;; \
        arm64) \
            himalaya_asset="himalaya.aarch64-linux.tgz"; \
            dbx_asset="dbx_v${DBX_VERSION}_linux_arm64.tar.gz"; \
            httpx_asset="httpx_v${HTTPX_VERSION}_linux_arm64.tar.gz"; \
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
        case "${archive}" in \
            *.zip) unzip -q "${archive}" -d "${tmpdir}/unpacked" ;; \
            *.tgz|*.tar.gz) tar -xf "${archive}" -C "${tmpdir}/unpacked" ;; \
            *) echo "Unsupported archive format for ${cli_name}: ${archive}" >&2; exit 1 ;; \
        esac; \
        bin_path="$(find "${tmpdir}/unpacked" -type f -name "${cli_name}" -perm -111 | head -n 1)"; \
        if [[ -z "${bin_path}" ]]; then \
            echo "Failed to locate the ${cli_name} binary in ${archive}" >&2; \
            exit 1; \
        fi; \
        install -m 0755 "${bin_path}" "/usr/local/bin/${cli_name}"; \
        rm -rf "${tmpdir}"; \
    }; \
    himalaya_url="${HIMALAYA_DOWNLOAD_URL:-https://cligrep.com/cli-releases/himalaya/v${HIMALAYA_VERSION}/${himalaya_asset}}"; \
    dbx_url="${DBX_DOWNLOAD_URL:-https://cligrep.com/cli-releases/dbx/v${DBX_VERSION}/${dbx_asset}}"; \
    httpx_url="${HTTPX_DOWNLOAD_URL:-https://cligrep.com/cli-releases/httpx/v${HTTPX_VERSION}/${httpx_asset}}"; \
    install_cli "himalaya" "${himalaya_url}"; \
    install_cli "dbx" "${dbx_url}"; \
    install_cli "httpx" "${httpx_url}"

RUN mkdir -p /workspace /skills

WORKDIR /workspace
