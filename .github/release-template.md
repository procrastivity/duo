# Binary releases for macOS

This release ships standalone `duo` binaries for macOS. The binaries are **not codesigned or notarized yet**, so macOS Gatekeeper will quarantine them on first download. The instructions below walk through the one-time `xattr` workaround.

## 1. Pick the right binary

Check your CPU architecture:

```sh
uname -m
```

- `arm64` → download **`duo-darwin-arm64`** (Apple Silicon: M1/M2/M3/M4)
- `x86_64` → download **`duo-darwin-x64`** (Intel Macs)

## 2. Remove the macOS quarantine flag

After downloading, run the matching command from the directory containing the binary:

```sh
# Apple Silicon
xattr -d com.apple.quarantine ./duo-darwin-arm64

# Intel
xattr -d com.apple.quarantine ./duo-darwin-x64
```

If you skip this step, macOS will refuse to launch the binary with a "cannot be opened because the developer cannot be verified" error.

## 3. Make it executable and test

```sh
chmod +x ./duo-darwin-arm64   # or duo-darwin-x64
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

- A `curl | sh` installer that downloads the right binary and runs the dequarantine step for you.
- A Homebrew tap so you can `brew install` Duo.

Until those land, the steps above are the supported install path on macOS.

## Checksums (optional)

SHA256 checksums for the attached binaries can be added below post-publish via the GitHub release UI:

```
<sha256>  duo-darwin-arm64
<sha256>  duo-darwin-x64
```
