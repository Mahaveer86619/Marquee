# Marquee Link

Marquee Link connects installations on different networks directly, without a central server. It carries co-watch synchronization, chat, a friend activity feed and shared recommendation signals. It never carries media.

Status: designed, not implemented. Link settings will be available in the application settings.

## Principles

- **No hosting.** Marquee does not require any server operated by the project or by the user. At most it uses free public infrastructure operated by others, and each such component is optional or replaceable.
- **Identity above transport.** Each profile has its own Ed25519 key. Friends are recognized by their pinned keys, whatever network path connects them.
- **Metadata only.** Peers exchange feed entries, room state, chat and presence.

## Transports

| Transport | Role | Notes |
|---|---|---|
| libp2p | Default | Local network discovery, public peer routing, NAT traversal by hole punching, and public relays as a last resort. No account required. |
| Tailscale (embedded) | Optional | More reliable on strict or carrier-grade NAT. Requires a Tailscale account per user. |
| Existing overlay networks | Automatic | If both users are already on the same Tailscale or ZeroTier network, those addresses are used directly. |

## Pairing

Pairing is done once per friend:

1. One user shows a pairing code, as a QR code or as text. It contains their public key, network addresses and a single-use secret that expires after ten minutes.
2. The other user scans or pastes the code. The two installations connect and exchange keys.
3. Both screens display the same short verification code. The users compare it, which protects against interception or tampering.
4. Each side pins the other's key. Sharing starts disabled except for co-watch invitations; each user then chooses what to share.

After pairing, installations reconnect automatically whenever both are online.

## Friend feed

Each profile keeps a signed, append-only log of the events it has chosen to share. When two friends are connected, they exchange any entries the other has not yet received, filtered by the scopes each has granted. In the first version, entries are exchanged only directly between author and recipient, never relayed through third parties.

Friend signals feed into recommendations as a "Friends loved" row and a limited boost in ranking, weighted by how similar each friend's taste is.

## Co-watching

- A room refers to a title by its universal reference, for example `tmdb:tv:1399:s01e01`, not by a file.
- Every participant plays their own copy. Differences between releases are corrected using the subtitle synchronization mapping of each file onto a shared reference timeline.
- The host's installation is authoritative for room state. If the host leaves, the next member takes over.
- Clients measure their clock offset from the host and correct drift. Small differences are corrected by adjusting playback speed slightly; larger ones by seeking.
- Subtitles, audio track and volume remain individual choices.
- Chat messages carry the playback position at which they were sent. Messages from beyond a viewer's current position are hidden until they reach that point.
