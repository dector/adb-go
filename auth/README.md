# adb-go/auth

Package `auth` provides explicit ADB host authentication credentials.

It loads or constructs RSA private-key based credentials for the ADB `AUTH`
exchange and prepares the corresponding NUL-terminated `AUTH RSAPUBLICKEY`
payload used by Android devices.

## Contents

- [Supported keys](#supported-keys)
- [Design constraints](#design-constraints)

Most users should access this through the root package:

```go
import adb "github.com/dector/adb-go"

credential, err := adb.LoadPrivateKey("/home/me/.android/adbkey")
```

Use the package directly when you need parsing or credential construction APIs:

```go
import "github.com/dector/adb-go/auth"

credential, err := auth.LoadPrivateKey("/home/me/.android/adbkey")
```

## Supported keys

- Unencrypted PEM-encoded RSA private keys.
- PKCS#1 `RSA PRIVATE KEY` blocks.
- PKCS#8 `PRIVATE KEY` blocks containing an RSA private key.
- 2048-bit Android ADB RSA keys.

Encrypted private keys are not supported in v0.

## Design constraints

Key handling is intentionally explicit. adb-go does not generate keys, persist
keys, search `~/.android`, rotate credentials, or integrate with OS keychains in
v0. Callers choose which credentials to load and where they come from.

See [`../docs/authentication.md`](../docs/authentication.md) for protocol flow
and key format details.
