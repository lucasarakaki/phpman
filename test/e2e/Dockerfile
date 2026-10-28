ARG BASE_IMAGE=debian:12
FROM ${BASE_IMAGE}

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        dbus \
        gnupg \
        procps \
        sudo \
        systemd \
        systemd-sysv \
    && rm -rf /var/lib/apt/lists/*

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
