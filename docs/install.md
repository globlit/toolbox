# Installation and verification

Install kubectl separately. Toolbox accepts configuration through `--config FILE`. Use [setup](setup.md) to connect an existing kubeconfig.

## Go installation

Use Go 1.26.10 or newer:

```sh
go install github.com/globlit/toolbox@latest
```

Install a specific published version:

```sh
go install github.com/globlit/toolbox@v0.4.0
```

Replace `v0.4.0` with a tag from [Releases](https://github.com/globlit/toolbox/releases). The workflow starts at `v0.4.0`. These public commands require the module migration and its first tag to be pushed.

Go installs the executable in `GOBIN`, or in `$(go env GOPATH)/bin` when `GOBIN` is unset. Add that directory to `PATH`, then check the version:

```sh
toolbox version
```

A versioned installation reports its module version, such as `toolbox v0.4.0`. `@latest` selects the latest eligible module version. The command package at `github.com/globlit/toolbox/cmd/toolbox` remains available. [Go installation reference](https://go.dev/ref/mod#go-install).

## Binary downloads

Download from [Releases](https://github.com/globlit/toolbox/releases/latest). Go is not required to run a release binary.

| System | Processor | Asset |
| --- | --- | --- |
| Linux | x86-64 / AMD64 | `toolbox-linux-amd64` |
| Linux | ARM64 | `toolbox-linux-arm64` |
| macOS / Darwin | Intel x86-64 | `toolbox-darwin-amd64` |
| macOS / Darwin | Apple Silicon ARM64 | `toolbox-darwin-arm64` |
| Windows | x86-64 / AMD64 | `toolbox-windows-amd64.exe` |
| Windows | ARM64 | `toolbox-windows-arm64.exe` |

32-bit x86 is not supported. Each release also includes `VERSION`, `SHA256SUMS`, and `SHA256SUMS.sigstore.json`.

## Verify on Linux or macOS

Install [Cosign](https://docs.sigstore.dev/cosign/system_config/installation/) first. Use Cosign v3.1.4 or newer. Choose a published version and the asset for your machine. This example uses Linux x86-64:

```sh
set -eu
version=v0.4.0
asset=toolbox-linux-amd64
base="https://github.com/globlit/toolbox/releases/download/$version"
for file in "$asset" VERSION SHA256SUMS SHA256SUMS.sigstore.json; do
  curl --fail --location --output "$file" "$base/$file"
done
cosign verify-blob --bundle SHA256SUMS.sigstore.json \
  --certificate-identity 'https://github.com/globlit/toolbox/.github/workflows/ci.yml@refs/heads/master' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  SHA256SUMS
awk -v asset="$asset" '$2 == asset || $2 == "VERSION"' SHA256SUMS > selected-checksums.txt
test "$(wc -l < selected-checksums.txt | tr -d ' ')" = 2
```

Check the binary and version file on Linux:

```sh
sha256sum --strict -c selected-checksums.txt
test "$(cat VERSION)" = "$version"
```

On macOS, use:

```sh
shasum -a 256 -c selected-checksums.txt
test "$(cat VERSION)" = "$version"
```

Both checksum entries must report `OK`. Stop if signature verification, a checksum, or the version comparison fails. After successful verification, install into a directory on `PATH`:

```sh
mkdir -p "$HOME/.local/bin"
install -m 0755 "$asset" "$HOME/.local/bin/toolbox"
"$HOME/.local/bin/toolbox" version
```

The signature authenticates the checksum manifest with the GitHub Actions workflow identity. The checksums bind the binary and version file to that manifest. Always check the exact identity and issuer. [Sigstore verification reference](https://docs.sigstore.dev/cosign/verifying/verify/).

## Verify on Windows

Install Cosign first. Run this PowerShell example for Windows x86-64. Change `$asset` to `toolbox-windows-arm64.exe` for ARM64:

```powershell
$ErrorActionPreference = 'Stop'
$version = 'v0.4.0'
$asset = 'toolbox-windows-amd64.exe'
$base = "https://github.com/globlit/toolbox/releases/download/$version"
foreach ($file in @($asset, 'VERSION', 'SHA256SUMS', 'SHA256SUMS.sigstore.json')) {
  Invoke-WebRequest "$base/$file" -OutFile $file
}
cosign verify-blob --bundle SHA256SUMS.sigstore.json `
  --certificate-identity 'https://github.com/globlit/toolbox/.github/workflows/ci.yml@refs/heads/master' `
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' SHA256SUMS
if ($LASTEXITCODE -ne 0) { throw 'Signature verification failed' }
foreach ($file in @($asset, 'VERSION')) {
  $entries = @(Get-Content SHA256SUMS | Where-Object { ($_ -split '  ', 2)[1] -ceq $file })
  if ($entries.Count -ne 1) { throw "Missing or duplicate checksum for $file" }
  $expected = ($entries[0] -split '  ', 2)[0]
  if ($expected -cnotmatch '^[a-f0-9]{64}$') { throw 'Invalid checksum' }
  if ((Get-FileHash $file -Algorithm SHA256).Hash.ToLowerInvariant() -cne $expected) {
    throw "Checksum mismatch for $file"
  }
}
if ((Get-Content VERSION -Raw).Trim() -cne $version) { throw 'Version mismatch' }
Copy-Item $asset toolbox.exe
.\toolbox.exe version
```

Place the verified `toolbox.exe` in a directory on `PATH`.

## Release automation

The [workflow](../.github/workflows/ci.yml) verifies Linux, macOS, and Windows tests. It builds all six targets. After successful checks on the current default-branch commit in `globlit/toolbox`, the release job selects the next stable patch version and builds version-stamped binaries. The first tag is `v0.4.0`, followed by `v0.4.1`, `v0.4.2`, and so on.

Release jobs run serially. Superseded commits are skipped. Reruns reuse an existing tag on the same commit. Published releases remain unchanged. Failed draft uploads can be retried.

GitHub release requests retry HTTP 429 and 5xx errors up to five attempts. Retries check whether the release is missing, draft, or published. Other lookup failures stop the job. After an outage, rerun the failed release job to complete the existing version.

The job hashes all binaries and `VERSION`, signs `SHA256SUMS` through GitHub's OpenID Connect identity, and verifies the signature before publishing. No signing private key is configured in repository secrets. Only the release job receives `contents: write` and `id-token: write`. Repository rules must permit its tag creation and release publication. [Sigstore signing reference](https://docs.sigstore.dev/cosign/signing/signing_with_blobs/).

The expected signing identity above uses the current `master` branch. If the default branch or workflow path changes, update the documented identity before publishing from that location. Do not replace the exact identity with an unrestricted regular expression.

Pull requests and other branches produce unsigned development artifacts. They do not publish releases or receive signing permissions. Cosign verification applies to downloaded release assets. `go install` uses Go's module download verification instead.

Build release-shaped files locally without publishing or signing:

```sh
go run ./scripts/build -version v0.4.0
```

Output appears in `dist/`. Local builds omit the Sigstore bundle. Do not claim that local builds were signed by GitHub Actions.
