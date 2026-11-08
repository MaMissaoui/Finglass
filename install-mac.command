#!/bin/bash
# Double-click in Finder to install the newest published Finglass release into /Applications.
# It needs no Rust. Use --from-source to build the checkout in this folder instead; that needs
# Rust 1.92+ and cargo-packager 0.11.8. A previous Finglass.app goes to the Trash. Quit Finglass first.
# Same as running `scripts/install-mac.sh` in this folder; extra arguments go to it.
# Arguments: [--install-dir <folder>] [--version <tag>] [--reinstall] [--from-source] [--force]
#            [--no-update] [--check-only] [--help]

cd "$(dirname "$0")" || exit 1

finish() {
    echo
    read -r -n 1 -s -p "Press any key to close this window."
    echo
}
trap finish EXIT

echo "Installing the latest Finglass release ..."
echo "No Rust is needed. Use --from-source to build this checkout instead; that needs Rust 1.92+"
echo "and cargo-packager 0.11.8."
echo

if ! ./scripts/install-mac.sh "$@"; then
    echo
    echo "Installation failed; see the messages above. Nothing in /Applications was changed unless they say so."
    exit 1
fi

echo
echo "Done. Start Finglass from Applications, Launchpad or Spotlight."
