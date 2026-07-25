# adb-go Implementation Plan

This plan is organized as small milestones. Each milestone should be implemented as one focused Conventional Commit.

## Progress

- Current milestone: Milestone 32 — Expose authentication through the high-level client.
- Completed milestone range: Milestones 17–29 completed the initial CLI shell/push/pull work, CLI documentation, Linux USB transport design, transport abstraction, Linux USB discovery, Linux usbfs bulk transport, high-level USB connection API, CLI USB connection option, USB documentation, and adb-go-specific target listing.
- Active focus: ADB authentication. Authenticated devices currently return `ErrAuthRequired` over both TCP and USB; the next slice should add explicit host credentials first, then wire those credentials into the protocol handshake, client API, CLI, and docs in separate reviewable milestones.
- Completed USB direction: Linux-only first, using the kernel usbfs interface under `/dev/bus/usb` behind build tags. This remains pure Go because it talks to device files and ioctls directly instead of linking native USB libraries.

## Milestone template

Use this shape when promoting new work into the active plan:

```markdown
## Milestone N — Short milestone title

Status: Not started

Commit: `type(scope): concise commit subject`

Tasks:

- [ ] First small, reviewable task
- [ ] Second small, reviewable task
- [ ] Documentation or API update, if relevant

Tests:

- [ ] Focused unit or integration tests for the new behavior
- [ ] `go test ./...` passes

Done when:

- [ ] One clear reviewable outcome describes why the milestone is complete
```

Milestone rules:

- Keep each milestone small enough to review as one merge request.
- Implement only the next incomplete milestone.
- Mark a milestone `Implemented` when it is ready for human review, even before commit.
- Use the listed Conventional Commit message, or update the milestone before committing if the scope changes.
- Do not keep fully implemented milestone bodies in this file long term; summarize completed ranges in `Progress` instead.

## Milestone 30 — Add authentication package and explicit credential loading

Status: Implemented

Commit: `feat(auth): add adb rsa key loading`

Tasks:

- [x] Add an `auth` package that represents ADB host credentials without coupling them to TCP, USB, or the high-level client.
- [x] Load unencrypted RSA private keys from explicit filesystem paths, supporting the private-key encodings used by common `~/.android/adbkey` files.
- [x] Generate the ADB public-key payload expected by `AUTH` public-key exchange, including the trailing NUL required on the wire.
- [x] Keep key loading explicit; do not create, persist, rotate, or discover keys in this milestone.

Tests:

- [x] Unit tests cover valid key loading, unsupported/malformed key files, and deterministic ADB public-key payload generation.
- [x] `go test ./...` passes

Done when:

- [x] Callers can load an existing ADB RSA private key and obtain a signer/public-key payload suitable for a later protocol handshake milestone.

## Milestone 31 — Add authenticated protocol handshake

Status: Implemented

Commit: `feat(protocol): add adb authentication handshake`

Tasks:

- [x] Add protocol-level handshake options for supplying one or more auth signers/public keys while preserving the existing unauthenticated `Handshake(ctx)` behavior.
- [x] Handle `AUTH TOKEN` by sending an `AUTH SIGNATURE` response and continue the handshake until `CNXN`, another auth challenge, or rejection.
- [x] Handle public-key offer fallback with `AUTH RSAPUBLICKEY` when configured and document when callers should expect the device authorization prompt.
- [x] Preserve `ErrAuthRequired` for missing credentials, unsupported auth packets, exhausted credentials, or peers that never complete authentication.
- [x] Extend `internal/fakeadb` so tests can require AUTH before CNXN.

Tests:

- [x] Protocol tests cover successful token signing, public-key fallback, no-credential `ErrAuthRequired`, rejected credentials, and malformed AUTH packets.
- [x] `go test ./...` passes

Done when:

- [x] A protocol connection can complete the CNXN handshake against an auth-requiring fake ADB peer when supplied valid explicit credentials.

## Milestone 32 — Expose authentication through the high-level client

Status: Not started

Commit: `feat(client): add authenticated connect options`

Tasks:

- [ ] Add client connection options that allow callers to provide explicit auth credentials for both TCP and USB connection paths.
- [ ] Preserve existing `Connect`, `ConnectTCP`, and USB helper defaults so callers that do not opt into auth still see `ErrAuthRequired`.
- [ ] Re-export the stable auth types or constructors from the root package when they are part of the high-level API.
- [ ] Add examples showing explicit key loading and authenticated connection without implying command/path safety guarantees.

Tests:

- [ ] Client tests cover TCP and fake USB/auth transport success with credentials and `ErrAuthRequired` without credentials.
- [ ] Example tests compile.
- [ ] `go test ./...` passes

Done when:

- [ ] Application code can authenticate to TCP or USB devices by loading credentials explicitly and passing them through the high-level API.

## Milestone 33 — Add CLI authentication support

Status: Not started

Commit: `feat(cli): add adb auth key option`

Tasks:

- [ ] Add a CLI flag for an explicit ADB private key path and thread it through every command that opens a device connection.
- [ ] Keep default CLI behavior unchanged when no key flag is supplied.
- [ ] Return a clear user-facing error when a device requires auth and no key was supplied, or when the supplied key cannot authenticate.
- [ ] Avoid writing or generating key material from the CLI in this milestone.

Tests:

- [ ] CLI tests cover key flag parsing, propagation to connection setup, and no-key `ErrAuthRequired` messaging.
- [ ] `go test ./...` passes

Done when:

- [ ] CLI users can run existing supported commands against auth-requiring devices by explicitly providing an existing ADB private key.

## Milestone 34 — Document authentication setup and limitations

Status: Not started

Commit: `docs: document adb authentication setup`

Tasks:

- [ ] Update README examples and limitations to describe explicit authentication support for TCP and Linux USB.
- [ ] Add docs explaining how ADB authentication works at a high level: token challenge, RSA signature, public-key authorization prompt, and final CNXN.
- [ ] Document supported key formats, explicit key-path usage in library and CLI, and unsupported behaviors such as key generation or keychain integration.
- [ ] Explain security considerations: key files are sensitive, commands and paths are caller-controlled, and adb-go does not log by default.

Tests:

- [ ] Documentation examples compile where applicable.
- [ ] `go test ./...` passes

Done when:

- [ ] Users can understand when authentication is needed, how to supply an existing key, and what adb-go intentionally does not manage.

## Deferred milestones

These remain out of the active plan unless promoted into a concrete milestone using the template above.

### Cross-platform USB

Potential future commit series depends on platform research:

- `feat(usb): add macos usb transport`
- `feat(usb): add windows usb transport`

These may require different OS APIs from Linux usbfs. Preserve the pure-Go preference and avoid cgo/native dependencies unless a future decision changes this.
