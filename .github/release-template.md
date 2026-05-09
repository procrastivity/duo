# Binary releases for macOS

This release ships a standalone `duo` binary for **macOS on Apple Silicon (arm64)**. The binary is **not codesigned or notarized yet**, so macOS Gatekeeper will quarantine it on first download. The instructions below walk through the one-time `xattr` workaround.

> **Intel Macs (`x86_64`) are not currently supported.** No `duo-darwin-x64` binary is attached to this release.

## 1. Confirm your architecture

```sh
uname -m
```

You should see `arm64`. Then download **`duo-darwin-arm64`** from the Assets list below.

## 2. Remove the macOS quarantine flag

From the directory containing the downloaded binary:

```sh
xattr -d com.apple.quarantine ./duo-darwin-arm64
```

If you skip this step, macOS will refuse to launch the binary with a "cannot be opened because the developer cannot be verified" error.

## 3. Make it executable and test

```sh
chmod +x ./duo-darwin-arm64
./duo-darwin-arm64 --help
```

You should see Duo's help output. From here, rename or move the binary onto your `PATH` however you prefer. A user-writable location like `~/.local/bin/duo` avoids needing `sudo`:

```sh
mkdir -p ~/.local/bin
mv ./duo-darwin-arm64 ~/.local/bin/duo
```

If `~/.local/bin` is not on your `PATH`, add it (e.g. `export PATH="$HOME/.local/bin:$PATH"` in your shell rc). Installing to `/usr/local/bin` also works but typically requires `sudo`.

## What's coming next

Future releases will offer easier install paths. **None of these are available yet:**

- A `curl | sh` installer that downloads the binary and runs the dequarantine step for you.
- A Homebrew tap so you can `brew install` Duo.

Until those land, the steps above are the supported install path on macOS.

## Checksums (optional)

SHA256 checksums for the attached binaries can be added below post-publish via the GitHub release UI:

```
<sha256>  duo-darwin-arm64
```
