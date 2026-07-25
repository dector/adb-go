# ADB authentication in adb-go

ADB authentication protects a device from accepting commands from unknown hosts.
When USB debugging is enabled and the device is not already configured to trust a
host key, the initial protocol handshake does not go straight from `CNXN` to
`CNXN`. Instead, the device sends an `AUTH` challenge.

The high-level flow is:

1. adb-go opens a TCP or Linux USB transport and sends the host `CNXN` packet.
2. An authenticated device replies with `AUTH TOKEN`. The token is random device
   data for this handshake.
3. adb-go signs the token with an explicitly supplied RSA private key and sends
   `AUTH SIGNATURE`.
4. If the device already trusts the corresponding public key, it replies with
   `CNXN` and the normal ADB session starts.
5. If the signature is not trusted, adb-go can send `AUTH RSAPUBLICKEY` using the
   same credential. A real Android device may then show an authorization prompt.
   Once the user accepts the prompt, the device completes the handshake with
   `CNXN`.

adb-go intentionally keeps key management explicit. It can load existing
unencrypted RSA private keys, such as common `~/.android/adbkey` files, but it
does not create keys, rotate keys, install keys, integrate with OS keychains, or
search default locations automatically.

Supported private-key encodings:

- PEM `RSA PRIVATE KEY` / PKCS#1
- PEM `PRIVATE KEY` / unencrypted PKCS#8 containing an RSA private key

Encrypted private keys and non-RSA keys are not supported yet.

Library example:

```go
ctx := context.Background()
credential, err := adb.LoadPrivateKey("/home/me/.android/adbkey")
if err != nil {
    return err
}
client, err := adb.ConnectTCPWithOptions(ctx, "127.0.0.1:5555", adb.ConnectOptions{
    AuthCredentials: []adb.AuthCredential{credential},
})
if err != nil {
    return err
}
defer client.Close()
```

For Linux USB, pass the same credential through `adb.USBOptions`:

```go
client, err := adb.ConnectUSB(ctx, adb.USBOptions{
    DevicePath:      "/dev/bus/usb/001/002",
    AuthCredentials: []adb.AuthCredential{credential},
})
```

CLI example:

```sh
adb-go shell --auth-key ~/.android/adbkey --addr 127.0.0.1:5555 getprop ro.product.model
adb-go shell --auth-key ~/.android/adbkey --usb-path /dev/bus/usb/001/002 getprop ro.product.model
```

Treat ADB private-key files as sensitive credentials. A host with access to a
trusted key can ask a trusted device to run shell commands and read or write data
through supported ADB services. adb-go does not log protocol data by default, but
callers are still responsible for protecting key paths, shell commands, and file
paths they provide.
