# Phase 2 Reachy Pose Bridge Contract

This specification defines the local-network WebSocket wire contract between `TinkeringDao/cronos64` (`echod/internal/lib/actuate` and `echod/internal/feature/robot`) and `TinkeringDao/actlatch` (built on `pollen-robotics/reachy_mini` SDK v1.13.0).

## Scope: Phase 1 vs. Phase 2

- **Phase 1 (Standalone Cronos Companion):** `echod` fuses voice turn state, stereo 2-mic TDOA azimuth, demand-gated front camera luma gaze, touch gestures, and `mtktscpu` thermal posture locally (`echod/internal/feature/robot/robot.go`), rendering the companion face on the Cronos display at 60 Hz (`echod/internal/feature/display/robot_face.go`) while wiring `Robot.actuate` to `actuate.NoopTransport{}`. No external robot hardware is required.
- **Phase 2 (Bridged Physical Reachy):** `Robot.SetTransport(...)` (wrapping `GuardedSender.SetTransport(...)`) swaps `NoopTransport{}` for `actuate.NewWebSocketTransport("ws://<lan-host>:<port>/v1/companion/pose")`, streaming clamped pose and expression frames to `TinkeringDao/actlatch` over the local LAN or RFC 6598 overlay mesh.
- **Physical Cronos Bring-Up Gate:** Before flashing a physical Cronos unit, verify the pre-flash partition backup (`p1..p11`, `p14`, `p15`, `mmcblk0boot0`, `mmcblk0boot1`, untouched `p3` `lk`, `ANDROID!` + `Linux version 4.9.337` in `p9`, and `SHA256SUMS`) with:

  ```sh
  python3 tools/verify-cronos-backup.py <backup_dir>
  ```

## Transport & Endpoint

- **Protocol:** WebSocket (`ws://` or `wss://`), UTF-8 JSON text frames (`websocket.TextMessage`), one JSON object per message.
- **Endpoint Path:**

  ```text
  ws://<lan-host>:<port>/v1/companion/pose
  ```

- **Example Local Endpoints (RFC 1918 / RFC 6598 / IPv6 ULA only):**
  - `ws://10.0.0.42:8765/v1/companion/pose`
  - `ws://[fd7a:115c:a1e0::42]:8765/v1/companion/pose`
- **Sender Timing & Connection Lifecycle (`echod/internal/lib/actuate/actuate.go`):**
  - Lazy dial on first `Send`, `5 s` WebSocket handshake timeout, `2 s` per-frame write deadline.
  - On write failure, `WebSocketTransport` closes the socket and clears the handle so the next frame triggers a fresh dial.
  - Maximum outbound frame cadence is `30 Hz` (`MinFrameInterval = 33 ms`). Frames submitted within `< 33 ms` of the previous accepted frame are dropped silently by `actuate.GuardedSender` without advancing `seq`.

## Local-Only Security Boundary (`netguard.LocalOnlyDialer`)

All outbound bridge connections must pass `netguard.LocalOnlyDialer(5 * time.Second)` (`echod/internal/lib/netguard/localonly.go`) with `Proxy: nil` on `websocket.Dialer`:

- Dial validation executes in `net.Dialer.Control` after DNS resolution and before any packet leaves the interface, blocking DNS-rebinding and HTTP-redirect bypasses.
- Allowed destination ranges (`netguard.IsLANOrOverlay`):
  - RFC 1918 private IPv4 (`10.0.0.0/8`, `172.16.0.0/12`, and the private `/16` block)
  - RFC 6598 shared/overlay mesh IPv4 (`/10` CGNAT/overlay block, e.g., Tailscale / WireGuard)
  - IPv6 Unique Local Addresses (`fc00::/7`)
- Explicitly refused (`netguard.Refused` / non-local checks): loopback (`127.0.0.0/8`, `::1`), device-own addresses, link-local, multicast, and all public/internet-routable destinations.

## JSON Frame Schema

Every frame serialized by `actuate.WebSocketTransport` matches the `actuate.Pose` struct fields and JSON tags byte-for-byte:

```go
type Pose struct {
	Seq      uint64  `json:"seq"`
	Yaw      float64 `json:"yaw"`
	Pitch    float64 `json:"pitch"`
	Roll     float64 `json:"roll"`
	Mood     string  `json:"mood"`
	Speaking float64 `json:"speaking"`
	TTLMS    int     `json:"ttl_ms"`
}
```

### Example Frame

```json
{"seq":1,"yaw":12.4,"pitch":-4.0,"roll":2.1,"mood":"curious","speaking":0.35,"ttl_ms":500}
```

### Field Specifications

| Field | JSON Type | Range / Allowed Values | Source & Clamping Semantics |
| --- | --- | --- | --- |
| `seq` | integer (`uint64`) | `>= 1`, strictly monotonic per sender instance | Assigned by `GuardedSender` only when a frame passes the `33 ms` rate gate. |
| `yaw` | number (`float64`) | `[-45.0, +45.0]` degrees (`MaxYawDeg = 45.0`) | Head pan (`+` right, `-` left). Phase 1 `robot.emitPose` maps `snap.LookX * 35.0`. Non-finite (`NaN`/`Inf`) clamped to `0`. |
| `pitch` | number (`float64`) | `[-25.0, +25.0]` degrees (`MaxPitchDeg = 25.0`) | Head tilt (`+` up, `-` down). Phase 1 `robot.emitPose` maps `-snap.LookY * 18.0`. Non-finite clamped to `0`. |
| `roll` | number (`float64`) | `[-45.0, +45.0]` degrees (`MaxRollDeg = 45.0`) | Head cant. Phase 1 `robot.emitPose` sets `8.0` for `curious`, `4.0 * snap.PetGlow` for `happy`, `0.0` otherwise. Non-finite clamped to `0`. |
| `mood` | string | One of 8 values (see table below) | String value of `robot.Mood` (`echod/internal/feature/robot/robot.go`). |
| `speaking` | number (`float64`) | `[0.0, 1.0]` normalized | Mouth/speech envelope (`snap.MouthOpen`), active during `speaking` state; clamped to `[0.0, 1.0]`, non-finite to `0`. |
| `ttl_ms` | integer (`int`) | `> 0`, default `500` (`DefaultTTLMS = 500`) | Receiver deadman expiration window in milliseconds; `GuardedSender` substitutes `500` whenever `TTLMS <= 0`. |

### Enumerated `mood` Values

Must match `robot.Mood` constants in `echod/internal/feature/robot/robot.go`:

1. `calm` — idle neutral resting state
2. `curious` — attentive orienting / post-startle inspection (`roll = +8.0` deg)
3. `happy` — forehead pet interaction glow (`roll = 4.0 * PetGlow` deg)
4. `listening` — active wake/voice capture pulse
5. `thinking` — LLM/tool turn processing
6. `speaking` — active TTS playback (`speaking` in `[0.0, 1.0]`)
7. `sleepy` — panel dark / sleep state via `Robot.SetAsleep(true)` (thermal throttling sets `Snapshot.ThermalDegraded` and displays `"Thermal"` on the face top bar without changing `mood`)
8. `startled` — rapid center double-tap reaction (`600 ms` transient)

## Receiver Contract (`TinkeringDao/actlatch`, `reachy_mini` v1.13.0)

### 1. Frame Validation & Sequence Guard

- Parse JSON text frames and ignore/reject frames missing numeric `seq` or containing `NaN`/`Inf`.
- Maintain `last_seq` per WebSocket connection (resetting `last_seq = 0` when a new connection opens). Drop stale or duplicate frames where `seq <= last_seq`.
- Independently enforce mechanical bounds (`yaw` `[-45, +45]`, `pitch` `[-25, +25]`, `roll` `[-45, +45]`, `speaking` `[0.0, 1.0]`) before passing targets to `reachy_mini`.
- Unknown `mood` strings must degrade safely to `calm` without raising an unhandled exception or dropping the stream.

### 2. Deadman Neutral-Return (`500 ms` Watchdog)

- On each valid frame, arm/reset a deadman timer using the frame's `ttl_ms` (`500 ms` default, clamped to a safe operational ceiling on the receiver).
- If **`ttl_ms` (`500 ms`) elapses with no fresh valid frame** (silence) **OR** when the WebSocket connection closes or errors:
  - Transition smoothly back to neutral pose (`yaw = 0.0`, `pitch = 0.0`, `roll = 0.0`, `mood = "calm"`, `speaking = 0.0`) over a gentle interpolation window (`~300–500 ms`) via `reachy_mini` rather than snapping joints or holding a stale deflection.

### 3. `pollen-robotics/reachy_mini` SDK v1.13.0 Mapping

- **Head Orientation:** Convert `(yaw, pitch, roll)` in degrees into the SDK head target (`create_head_pose(yaw=..., pitch=..., roll=..., degrees=True)` passed to `set_target(head=...)`) paced smoothly to match the `<= 30 Hz` input stream.
- **Mood & Antennas:** Map `mood` onto Reachy Mini antenna offsets and subtle idle motion (e.g., lifted perked antennas on `curious`/`listening`, asymmetric playful dip on `happy`, lowered relaxed posture on `sleepy`, quick recoil on `startled`).
- **Speech Envelope (`speaking`):** Modulate subtle head-nod/antenna micro-amplitude proportional to `speaking` (`[0.0, 1.0]`) when `mood == "speaking"`, decaying smoothly to `0.0` at turn end or deadman expiry.
