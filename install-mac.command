#!/bin/bash
# Double-click in Finder to build the release checked out here and install it into /Applications.
# The first build takes a few minutes and needs Rust 1.92+ and cargo-packager 0.11.8.
# A previous Finglass.app goes to the Trash. Quit Finglass first.
# Same as running `scripts/install-mac.sh` in this folder; extra arguments go to it.
# Arguments: [--install-dir <folder>] [--force]

cd "$(dirname "$0")" || exit 1

finish() {
    echo
    read -r -n 1 -s -p "Press any key to close this window."
    echo
}
trap finish EXIT

echo "Building and installing Finglass — the first build takes a few minutes."
echo "Needs Rust 1.92+ and cargo-packager 0.11.8."
echo

if ! ./scripts/install-mac.sh "$@"; then
    echo
    echo "Installation failed; see the messages above. Nothing in /Applications was changed unless they say so."
    exit 1
fi

echo
echo "Done. Start Finglass from Applications, Launchpad or Spotlight."
