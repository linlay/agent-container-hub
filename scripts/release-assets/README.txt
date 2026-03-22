agent-container-hub release bundle

This bundle is intended for Linux host-process deployment.
It does not include container images or source code build tooling.

What is included:
- agent-container-hub binary
- VERSION
- .env.example
- configs/environments/ runtime configs
- start.sh / stop.sh
- systemd/agent-container-hub.service

Deployment steps:
1. Extract the tar.gz bundle.
2. Change into the extracted agent-container-hub directory.
3. Copy .env.example to .env and adjust paths, bind address, auth token, and ENGINE if needed.
4. Make sure docker or podman is installed and the service user can access the container engine.
5. Start with ./start.sh or ./start.sh --daemon.

systemd:
- A template unit is provided at systemd/agent-container-hub.service.
- Replace /opt/agent-container-hub with your real install path before enabling it.

Notes:
- configs/environments is treated as the live environment config source.
- data/rootfs and data/builds are kept outside the binary and should live on persistent storage in production.
- stop.sh only stops processes started by ./start.sh --daemon.
