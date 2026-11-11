# The Linux image scripts/ci-local.sh builds for its "linux" job: golangci-lint's Go image plus the
# GTK4/WebKitGTK 6 headers Wails v3 needs on Linux. Built locally and cached by Docker; never pushed.
FROM golangci/golangci-lint:v2.15.0
RUN apt-get update \
 && apt-get install -y --no-install-recommends pkg-config libgtk-4-dev libwebkitgtk-6.0-dev libsoup-3.0-dev \
 && rm -rf /var/lib/apt/lists/*
