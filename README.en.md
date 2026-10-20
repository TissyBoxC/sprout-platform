<div align="center">

<img src="assets/brand/sprout/brand_avatar.png" alt="Sprout House" width="180" />

# Sprout House

### Sprout Series · First Sprout

Modular AI early-education and companionship platform for young children

[简体中文](README.md) | English

[![Go](https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Flutter](https://img.shields.io/badge/Flutter-Dart%203.11-02569B?logo=flutter&logoColor=white)](https://flutter.dev/)
[![Vue 3](https://img.shields.io/badge/Vue%203-TypeScript-4FC08D?logo=vuedotjs&logoColor=white)](https://vuejs.org/)
[![ESP-IDF](https://img.shields.io/badge/ESP--IDF-ESP32--S3-E7352C?logo=espressif&logoColor=white)](https://docs.espressif.com/projects/esp-idf/en/stable/esp32s3/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white)](https://redis.io/)

<!-- COMMUNITY_LINKS_START: community links pending. -->
<!-- DOCUMENTATION_LINKS_START: documentation entry pending. -->

</div>

`sprout-platform` is the platform repository for Sprout House's first product,
Sprout Series · First Sprout. It contains the parent application, admin console,
backend services, realtime voice gateway, shared contracts, and local deployment
tooling. Firmware and the AI gateway fork remain independent repositories.

> Every capability starts disabled or in its most conservative state. Social
> features, camera, microphone upload, and data collection require explicit
> guardian activation. Child privacy, psychological safety, and physical safety
> take priority over feature delivery.

## Current Status

The repository version is `0.32.5`. Accounts, families, devices, content,
voice, notifications, OTA, the admin Feature Center, and cloud service-version
management have software-level closed loops. Capabilities that still depend on
physical hardware, production keys, or bench validation remain `[Partial]`.
Status is based on an accepted end-to-end capability, not on the presence of a
directory, interface, or placeholder file.

| Marker | Meaning |
| --- | --- |
| `[Implemented]` | The capability, error paths, authorization or privacy controls, and relevant tests exist. |
| `[Partial]` | A runnable skeleton, contract, or single-side capability exists, but the end-to-end flow is incomplete. |
| `[Not implemented]` | No acceptance-ready business implementation exists yet. Only planning, directories, or empty interfaces are present. |

Hardware boundary after the documentation audit:

- Code, contracts, and policy layers do not prove a hardware loop. Camera
  sensor/framebuffer capture, QR provisioning on a display, real screen and
  touch drivers, battery and cellular modules, motors, and positioning modules
  still require validation on the target board.
- The production configuration enables Secure Boot V2, Flash Encryption, OTA
  rollback, and anti-rollback, but eFuse provisioning, signing-key custody,
  production image flashing, real dual-slot OTA, power-loss recovery, and
  rollback bench tests have not been validated in this repository. Do not
  describe the secure-boot or OTA hardware path as production-verified.
- The current `esp32-s3-n16r8-production` configuration does not enable display
  policy, touch input, battery, cellular, motion, wearable location, or family
  linkage. Platform APIs and console pages for those capabilities do not make
  the default production firmware hardware-complete.

## Complete Feature Inventory

The inventory covers the foundation, mainstream, and differentiated layers
across P0, P1, and P2. Every item is in scope. Priority only controls delivery
order and never removes a capability.

### P0 Required Capabilities

| ID | Capability | Modules | Status | Current state |
| --- | --- | --- | --- | --- |
| P0-01 | Startup, version, error recovery | `system_core`, `module_registry`, `version_info`, `error_code`, `error_recovery`, `diagnostic_reporter` | `[Implemented]` | Firmware reports boot, module-failure, and recovery events; the platform validates and idempotently stores bounded history; the admin console shows health, boot history, recent failures, and recovery events. |
| P0-02 | Audio input | `audio_codec`, `audio_pipeline`, `voice_gateway` | `[Implemented]` | The firmware performs 16 kHz mono Opus encode/decode, I2S capture, post-capture noise reduction, echo cancellation, gain calibration, and VAD segmentation with speaker-reference loopback; the voice gateway implements short-lived device tokens, the realtime WebSocket audio session, Opus decode, ring buffering, inbound noise reduction/echo cancellation/gain calibration, adaptive VAD segmentation, ASR/LLM/TTS adapters with a conversation loop, and the voice control-frame contract with stable error codes. |
| P0-03 | Audio output | `audio_output`, `playback_queue`, `volume_control`, `prompt_tone`, `voice_gateway` | `[Implemented]` | The firmware mixes multiple paths by priority, enforces the guardian volume ceiling, keeps safety prompt tones audible while muted, overlays local prompt tones, and feeds the capture reference; the voice gateway routes TTS replies through a per-connection playback queue with preemption, resume, pause, mute, and cleanup, and reports mix failures with stable audio error codes. |
| P0-04 | Voice wake | `voice_wake`, `wake_feedback`, `voice_gateway` | `[Implemented]` | The firmware detects the wake word, suppresses false triggers, provides local feedback, tones, and LED state, and reports redacted wake events in the heartbeat; the voice gateway validates device wake control frames. |
| P0-05 | Voice session | `voice_session`, `voice_gateway` | `[Implemented]` | The device connects with a short-lived platform-issued token; a wake word can establish the listening session directly, the gateway drives a continuous listening to thinking to speaking to listening state machine and emits a `session_state` frame at each step, child speech during playback clears the current reply and returns to listening (barge-in, safety announcements preserved), and idle or duration limits end the session; firmware `voice_session` owns the WSS transport, SRAW/SRSV envelope handling, and audio capture/playback integration. |
| P0-06 | AI conversation | `conversation_context`, `child_prompt_profile`, `voice_gateway`, `sub2api_fork` | `[Implemented]` | The gateway `sub2api_client` streams OpenAI-compatible replies with retries and stable error mapping and keeps bounded per-session multi-turn context; firmware `conversation_context` stores bounded turn summaries and state mirroring, `child_prompt_profile` persists the age tier/content level and sends it at session start, and `sub2api_fork` validates the device origin tag, model allowlist, quota, and degradation on the completion and models routes and records redacted usage into the audit log. |
| P0-07 | Content | Content library, stories, nursery rhymes, poetry, English, encyclopedia, bedtime, age tiers | `[Implemented]` | The platform supports content drafts, review submission, approval or rejection, publishing, withdrawal, archival, and review history, verifies the asset SHA-256 on publish, and advances an atomic catalog revision; the parent app syncs incrementally by age tier and category, verifies downloads, caches packages, and removes withdrawn items; the admin console exposes the full content workflow; the firmware pulls the incremental device manifest with its session token, resumes downloads, verifies SHA-256, commits packages atomically to SPIFFS, and plays them at ambient priority. |
| P0-08 | Connectivity | `network_manager`, `device_provisioning`, `time_sync`, `cloud_auth`, `offline_fallback`, `provisioning_reporter`, `device_runtime_reporter` | `[Implemented]` | On top of BLE provisioning, device binding, time sync, network quality, and cloud authentication, firmware `provisioning_reporter` records provisioning start, Wi-Fi configuration, binding, time sync, reconnect, and auth-recovery steps into a bounded event buffer persisted across reboots and reports them in the heartbeat; the `offline_fallback` pending-telemetry counter is now NVS-persisted and clears only after authentication recovers and a heartbeat is accepted. The platform adds a provisioning-event audit table, `GET /api/v1/admin/devices/{device_id}/provisioning`, and `POST /api/v1/admin/devices/{device_id}/sessions/revoke`, and returns the current provisioning state, Wi-Fi configuration flag, session state, and last provisioned time from the runtime endpoints; the parent app renders staged provisioning progress, session-expiry prompts, and offline empty states, and the admin console provides a provisioning timeline and revoke action. Operator commands are published at QoS 1 as `device.command` to `sprout/v1/devices/{device_id}/commands` after they are persisted, with the authenticated HTTP poll as the broker-outage fallback and a shared idempotency key on `request_id`; the platform subscribes to `sprout/v1/devices/+/commands/ack` and applies acknowledgements through the same single-write idempotent path, while firmware `device_mqtt` uses mutual-TLS client certificates to subscribe to `sprout/v1/devices/{device_id}/commands`, parses commands against the shared contract, executes them through the same idempotent runtime path, and publishes acknowledgements at QoS 1 on `.../commands/ack`, keeping authenticated HTTP polling as the fallback when MQTT credentials are absent or the broker is unreachable. |
| P0-09 | Parent control | `parent_link`, `parent_policy`, `usage_report` | `[Implemented]` | On top of family-bound devices, the platform derives one effective policy from every child policy using most-restrictive aggregation (category intersection, minimum daily duration and volume, union of blocked periods) with a per-family revision, and the guardian update path carries `policy_version` for optimistic concurrency and returns 409 on a stale write. Devices read that policy with their session token and never regress to a lower revision. Firmware adds a `parent_control_runtime` gate (stable reasons: disabled period, category denied, daily limit reached, time untrusted) and a `usage_ledger` that handles cross-midnight rollover, day reset, reboot/power-loss durability, clock rollback, offline accumulation, and idempotent per-device-per-date upload, actually blocking wake, voice sessions, content download, and playback and re-syncing `child_prompt_profile` after refresh. The platform adds `device_usage_daily` plus `POST /api/v1/devices/{device_id}/runtime/usage`, `GET /api/v1/usage-reports`, and `GET /api/v1/admin/families/{parent_account_id}/usage-reports`, and filters the content catalog by the effective policy. The voice gateway consumes a production `content_policy` resolver with a per-device cache to enforce age tier, category allowlist, and crisis-intent handling server-side. The parent app gains a usage report view (trend, categories, blocked reasons, per-device breakdown, remaining allowance, conflict and unsaved-change prompts) and the admin console gains a read-only usage report view that distinguishes "not set" from a read failure. |
| P0-10 | Security and privacy | `privacy_guard`, `content_filter`, `transport_security`, `security_posture`, platform and voice security modules | `[Partial]` | Authentication, sessions, short-lived device tokens, administrator RBAC, per-route audit, input/output moderation, crisis intervention, deletion, consent withdrawal, and production secure-boot/flash-encryption posture checks are implemented and tested. eFuse provisioning, signing-key custody, production image flashing, debug-interface lockdown on finished units, and physical-device penetration testing remain incomplete. |
| P0-11 | OTA | `ota_manager`, `ota_download`, `ota_validate`, `ota_rollback` | `[Partial]` | The platform implements draft, publish, pause, withdraw, signature validation, canary rollout, deployment statistics, and verification of the real artifact on the shared download volume. Firmware implements chunked download, SHA-256 and signature validation, dual-slot installation, resumable transfer, stable failure codes, rollback, and anti-rollback configuration. Unified manifest policy fields, platform-authorized downgrade handling, real-network upgrades, power-loss recovery, dual-slot rollback, and eFuse version-increment bench tests are still required before claiming a hardware OTA loop. |
| P0-12 | Device interaction | Buttons, LED, prompt tones, volume, factory reset, status feedback | `[Implemented]` | Button gestures, LED state, local tones, guardian volume limits, protected local and remote factory reset, interaction reporting, and idempotent command acknowledgement are implemented. |

### P1 Mainstream Capabilities

| ID | Capability | Modules | Status | Current state |
| --- | --- | --- | --- | --- |
| P1-01 | Voice enhancement | Full duplex, far-field pickup, continuous conversation | `[Implemented]` | Firmware duplex capture, echo reference, barge-in, playback cleanup, and bounded voice-quality telemetry are implemented; the voice gateway maintains a continuity window and exposes bounded quality snapshots; guardian policy and the console expose the corresponding settings. Acoustic and far-field performance still requires a physical-room bench test. |
| P1-02 | Content operations | Packages, themes, updates, favorites, playback history | `[Implemented]` | Favorites, playback history, theme-package lifecycle, incremental catalog revisions, resumable transfers, SHA-256 verification, and optional firmware persistence modules are implemented with parent and admin UI. |
| P1-03 | Companionship | Emotion response, encouragement, growth records, habit reminders, long-term memory | `[Implemented]` | The platform adds a family-scoped companionship module: growth records (`GET/POST /api/v1/companionship/growth-records`, `DELETE .../{record_id}`, filtered by child/category/limit/before) and habit reminders (`GET/POST /api/v1/companionship/habit-reminders`, `PUT/DELETE .../{reminder_id}`, `HH:MM` time, ISO Monday–Sunday weekdays, empty set expands to every day), all family-scoped and validated into migration `0027`. The parent app adds a "Companionship" page to record milestones/encouragement/habits/emotions, filter the timeline by child, delete records, and edit reminders with a time picker, weekday chips and an enable toggle. Firmware adds `companionship` (bounded encouragement reply keyed to emotion, the local fallback when the AI reply is unavailable), `growth_record` (bounded persisted growth-record cache with per-child deletion), `habit_reminder` (bounded persisted reminder cache with a pure `habit_reminder_due_at` scheduler), and `long_term_memory` (writes only after guardian authorization, revoking clears every memory, per-entry and per-child deletion, `recall`/`list` return nothing without authorization). |
| P1-04 | English learning | Words, sentences, speaking practice, pronunciation feedback | `[Implemented]` | The platform adds an English learning module (migration `0028`): a lesson catalog grouped by age tier (`age_3_4/age_5_6/age_7_8`) with words (phonetic, translation, example) and sentence patterns (pattern, prompt, model answer), read-only to guardians (`GET /api/v1/english/lessons`, `GET .../lessons/{lesson_id}`); practice progress is family-scoped (`GET /api/v1/english/progress` returns per-lesson passed items, best and average scores plus per-item best scores, and `GET /api/v1/english/attempts`), while the device reports attempts with its session token (`POST /api/v1/devices/{device_id}/english/attempts`). Scores are clamped to 0-100 and the pass flag is derived from a 60-point threshold so a client cannot claim a pass it did not earn. The parent app adds an "English" page to browse lessons by age tier, open a lesson to review its words and sentences, show per-item best scores and overall completion, and filter progress by child. Firmware adds the `english_course` module, which persists a bounded lesson catalog (words and sentence patterns per lesson) and a local practice log with pronunciation scores, supporting lesson upsert/remove, attempt recording, and per-item best-score rollup so practice continues offline and reports back. |
| P1-05 | Parent app | Remote messages, playback requests, device status, content recommendations | `[Implemented]` | The remote message inbox with unread badge and timed on-device display, device status and firmware updates, on-demand playback (guardian `GET/POST /api/v1/playback/requests`, a status machine that drives device-side `acknowledge/complete`, and the `playback_request` firmware cache drained in order), and content recommendations (`GET /api/v1/content/recommendations`, ranked by favourite and recent categories, the child's age tier, and freshness while excluding already-favourited or played packages, surfaced as a "为你推荐" carousel with one-tap download under 内容管理) are all complete. |
| P1-06 | Local fallback | Offline stories, local commands, cached playback | `[Implemented]` | Firmware covers the offline grace window with recovery detection, offline content (`offline_content`, including cache corruption verification and rebuild), local commands, cached playback, and offline usage records (`usage_ledger` with a bounded pending queue that backfills idempotently per device and date). The platform adds local commands (migration `0030`: guardian `GET/PUT/DELETE /api/v1/local-commands`, device `GET /api/v1/devices/{device_id}/local-commands`) and a content offline download policy (migration `0031`: a platform-wide singleton cache cap and TTL via guardian `GET/PUT /api/v1/content/offline-policy`, device `GET /api/v1/devices/{device_id}/content/offline-policy`, plus a device applied-revision acknowledgement and a guardian view of lagging devices). All three surfaces have UI: "本地指令" and "离线下载" in the parent app, and "离线下载策略" in the admin console, which reads/writes the policy and shows each family's applied device revisions. Post-recovery backfill is served by the existing device usage upload keyed by `report_date`. |
| P1-07 | Diagnostics | System diagnostics, logs, crashes, network quality, temperature | `[Implemented]` | Firmware reports boot, module-failure, recovery, crash, and temperature events; the platform validates and aggregates them with retention windows, and the admin console offers diagnostic queries, crash views, windowed trends, and network-quality threshold alerts. The guardian app shows only an actionable stability hint. Crashes remain bounded, redacted signatures with no raw memory or backtraces. |
| P1-08 | Camera | Capture, object recognition, picture-book recognition, photo Q&A | `[Partial]` | Privacy-gated visual-task state machines, the voice-gateway vision pipeline, platform task/result APIs, and parent/admin pages are present. The firmware still lacks a real sensor/framebuffer capture provider, and the production capability does not enable the camera, so capture and recognition cannot be accepted on hardware. |

### P2 Differentiated Capabilities

| ID | Capability | Modules | Status | Current state |
| --- | --- | --- | --- | --- |
| P2-01 | Display | Expressions, animation, point-to-read, video playback | `[Partial]` | Screen policy, point-to-read storage, clamped video profiles, playback state, platform APIs, parent settings, and admin statistics exist. The production configuration does not enable the screen component or display capability, and real LVGL panel rendering has not been accepted on hardware. |
| P2-02 | Eye-care display | Brightness, color temperature, distance, duration policy | `[Partial]` | Policy, status reporting, and UI exist; real brightness, color-temperature, distance sensing, and duration enforcement have not been validated on target hardware. |
| P2-03 | Touch | Tap, swipe, gestures, haptic feedback | `[Partial]` | Platform policy, status, and UI exist; the production configuration does not enable touch capability, and the real controller, gestures, and haptics remain unverified. |
| P2-04 | Cellular | eSIM, data usage, remote wake, connectivity switching | `[Partial]` | Policy, status, and UI exist; cellular capability is disabled in production, and the eSIM/modem, data accounting, and network switching have no physical loop yet. |
| P2-05 | Battery | Charging, estimation, low power, deep sleep | `[Partial]` | Policy, status, and UI exist; battery capability is disabled in production, and charging, fuel gauge, low-power, and sleep behavior require power-board tests. |
| P2-06 | Motion | Motors, servos, walking, dancing, tail actions | `[Partial]` | Motion policy, safety gates, and UI exist; motion capability is disabled in production, and motors, servos, limits, and emergency stop require mechanical/electrical bench validation. |
| P2-07 | Form factors | Capability sets for multiple physical forms | `[Partial]` | The platform can describe form factors with capability sets and non-character names; physical module combinations, structural safety, and SKU-level acceptance are incomplete. |
| P2-08 | Video companionship | Video calls, remote companionship | `[Partial]` | Contacts, call state, device status, parent UI, and admin views are wired; real audio/video codecs, media gateway, weak-network handling, and device-side calling are not hardware-verified. |
| P2-09 | Wearables and location | Location, calling, geofence, SOS | `[Partial]` | Location policy, geofence, SOS state, and audit APIs exist; production does not enable the wearable-location module or a real positioning modem, so accuracy, geofence, SOS, and compliance remain unverified. |
| P2-10 | Multi-device | Family linkage, device mesh, content sync | `[Partial]` | Device groups, membership, routed messages, and content-sync APIs exist; production does not enable family linkage, and multi-device networking, offline conflicts, and cross-device consistency still need bench validation. |

### Layer Coverage

| Layer | Scope | Status | Notes |
| --- | --- | --- | --- |
| Foundation | Provisioning, wake, recording/playback, AI conversation, content, parent control, OTA | `[Partial]` | Audio, wake, device interaction, AI conversation, content, and parent control have software loops. OTA has platform, firmware, parent, and admin code, but real dual-slot upgrade, power-loss recovery, rollback, and eFuse checks remain unverified. |
| Mainstream | Continuous conversation, content operations, companionship, English, remote messages, offline fallback, diagnostics, camera | `[Partial]` | Continuous conversation, content operations, companionship, English, remote messages, offline fallback, and diagnostics have three-surface implementations. Camera lacks a real sensor/framebuffer provider and cannot be claimed as a hardware loop. |
| Differentiated | Display, touch, eye care, cellular, battery, motion, video, location, multi-device | `[Partial]` | Policy and status surfaces exist in the platform, parent app, and admin console; production firmware does not enable the corresponding hardware capabilities, so real panel, touch, modem, power, motor, location, and multi-device validation remain. |

### Supporting Modules

| Module | Purpose | Status | Current state |
| --- | --- | --- | --- |
| `module_registry` | Register, initialize, and stop optional modules | `[Implemented]` | Firmware supports module registration, initialization failure recording, and removable-module verification. |
| `config_store` | Store device configuration and capability sets | `[Implemented]` | Supports NVS string and binary key read/write and deletion for provisioning, binding, and volume persistence; production images still need NVS encryption enabled. |
| `playback_queue` | Manage playback priority, interruption, and resume | `[Implemented]` | Both firmware and the voice gateway queue by priority with preemption, unplayed-tail resume, pause, and cleanup; safety prompt tones cannot be interrupted or muted. |
| `volume_control` | Enforce guardian volume ceiling and local mute | `[Implemented]` | Supports volume 0 to 100, policy-ceiling clamping, persistence, mute, and saturating PCM gain. |
| `prompt_tone` | Generate local wake, capture, network, and safety prompt tones | `[Implemented]` | Generates nine local prompt tones, plays them by priority when a queue is available, and keeps safety prompt tones at the highest priority. |
| `alarm_reminder` | Scheduled reminders, alarms, and routines | `[Implemented]` | Time-based alarms, snooze, weekday expansion, bounded persistence, device reporting, platform APIs, and parent UI are implemented. |
| `bluetooth_audio` | Bluetooth speaker mode and pairing | `[Partial]` | Policy, adapter state machine, capability/statistics APIs, admin UI, and firmware schema gating exist; real radio, pairing, codec switching, reconnect, and audio routing remain unverified. |
| `learning_visualization` | Touch-screen learning content and visual feedback | `[Partial]` | Policy, state machine, bounded device status, parent settings, and admin statistics exist; production touch/display capabilities are disabled, so visual and touch behavior is not hardware-verified. |
| `long_term_memory` | Guardian-authorized long-term companionship memory | `[Implemented]` | Writes only after explicit guardian authorization; revoking authorization erases every memory, and `recall`/`list`/`count` return nothing without consent; minimized storage dedupes by child+key, supports per-entry and per-child deletion, persists a single binary envelope, and starts from an empty unauthorized state when the blob is missing or corrupt. |
| `image_privacy` | Local image processing and upload consent | `[Partial]` | Policy, guardian consent, upload gating, bounded status, and aggregate metrics exist; real local preprocessing and camera capture remain incomplete. |
| `video_session` | Video-call and remote-companionship media session | `[Partial]` | Session state, guardian/child consent gates, policy, status reporting, and admin statistics exist; real capture, codecs, media gateway, weak-network handling, and device-side indication remain unverified. |

### Engineering Foundation

The following items do not replace P0/P1/P2 capabilities, but they determine
whether later modules can be added, removed, and regression-tested independently.

| Capability | Status | Current state |
| --- | --- | --- |
| Shared contracts and generated types | `[Implemented]` | Identity, family, child, device, content, OTA, policy, event, MQTT, error, and UI-text schemas exist with Dart/TypeScript generated types and contract-drift gates. |
| Contract validation and CI | `[Implemented]` | Platform CI validates local deployment, contracts, generated types, Go services, Vue admin, and Flutter parent app. |
| Automated release | `[Implemented]` | Changing `VERSION` creates a Chinese release with Go binaries, admin-web artifacts, and contract archives. External repositories release independently. |
| Docker Compose | `[Implemented]` | Local compose supports PostgreSQL, Redis, MQTT/TLS, optional `sub2api`, `device_platform`, `voice_gateway`, and the local version-state worker. Cloud compose supports PostgreSQL, Redis, MQTT, Sub2API, device platform, voice gateway, admin web, read-only download, SFTP fallback, and `upgrade_worker`. |
| Request labels, logging, and redaction | `[Implemented]` | Services use request IDs, trace fields, access logs, and sensitive-field redaction. |
| Remote UI text | `[Implemented]` | Contract, draft/publish lifecycle, Ed25519 signature validation, incremental sync, admin review/publish UI, and firmware application/fallback are implemented. |
| Feature Center | `[Implemented]` | A typed registry covers product and platform capabilities with validated editable settings, read-only metrics, authoritative health probes, optimistic concurrency, audit records, and unavailable/unknown states that are not presented as healthy. |
| Brand service version management | `[Implemented]` | The admin console lists every managed image's current and repository-latest version in service rows and can upgrade platform services and the AI gateway individually or in a batch; `upgrade_worker` is observed as part of the platform catalog, self-upgrade runs outside the admin container, and infrastructure images are only checked, never auto-upgraded. |
| `sub2api_fork` customization | `[Implemented]` | The fork keeps upstream behavior while adding strict tenant labels, child model/parameter policy, internal usage and policy APIs, redacted model-call auditing, and `sprout.*`/`SPROUT_` configuration prefixes; all custom behavior defaults off. |

### Feature Center

The admin console's Feature Center is the single registry for current and future
product and platform capabilities. Each entry has a stable `lower_snake_case`
ID, Chinese display name, category, owning service, lifecycle status,
configuration schema, authoritative health probe, and read-only metrics.
Editable values are validated by the backend; secrets reveal only configured or
unconfigured state. Config versions provide optimistic concurrency and a stale
write must reload. Configuration changes and manual checks record the
administrator, timestamp, and audit action. Health comes from a runtime probe,
persisted state, or an explicit not-configured result. `unknown`, `unavailable`,
and stale observations are never presented as healthy. See
[cloud deployment](deploy/cloud/README.md#feature-center) for operations and
troubleshooting.

## System Composition

| Project | Audience | Responsibility | Status |
| --- | --- | --- | --- |
| `apps/parent_app` | Parents and guardians | Login, family, child, device, policy, reports, remote messages, content, QR binding, software updates, OTA | `[Implemented]` |
| `apps/admin_web` | Brand operators and administrators | Accounts, families, children, devices, content, AI gateway, OTA, Feature Center, downloads, service versions, audit, monitoring | `[Implemented]` |
| `services/device_platform` | Platform internal | Parent and device APIs, device transport, content, policy, OTA, telemetry, Feature Center, audit | `[Implemented]` |
| `services/voice_gateway` | Platform internal | Realtime voice, ASR, TTS, content safety, `sub2api`, usage recording | `[Implemented]` |
| `services/sub2api_fork` | AI infrastructure | Model credentials, routing, quota, rate limits, billing, provider compatibility | `[Partial]` |
| `firmware` | Device | Wake, recording, playback, session, cache, network, policy, OTA | `[Partial]` |
| `packages/contracts` | Cross-project | HTTP, event, MQTT, capability, and UI-text contracts | `[Implemented]` |

## Boundaries

```text
Parent app ─────────────┐
                        ├──> device_platform ──> PostgreSQL / Redis / MQTT
Admin web ──────────────┘

Device ── MQTT/TLS ─────> device_platform
   │
   └── WebSocket ───────> voice_gateway ──> sub2api ──> model providers
                                  │
                                  ├──> ASR
                                  └──> TTS
```

- `parent_app` only calls `device_platform` and never connects to a device directly.
- `admin_web` only calls platform management APIs and never stores provider keys in the browser.
- `device_platform` owns child, family, device, content, policy, OTA, and audit data.
- `voice_gateway` owns realtime voice and AI adaptation, but not family or device master data.
- `sub2api` owns model credentials, routing, quota, and providers. It does not contain child or device data.
- `firmware` stores device identity, required configuration, and cache, but never an upstream model key.

## Workspace Layout

```text
sprout-platform/           current repository
  apps/
    parent_app/            Flutter guardian application
    admin_web/             Vue 3 admin console
  packages/
    contracts/             OpenAPI, event, MQTT, capability, and UI-text contracts
    go/                    Shared Go HTTP and observability packages
  services/
    device_platform/       Go device and family business service
    voice_gateway/         Go realtime voice and AI gateway
  deploy/                  Compose, PostgreSQL initialization, MQTT config
  tools/                   Bootstrap, contracts, certificates, local deployment
  workspace.lock.yaml      External repository revisions

sprout-firmware/           separate repository, local path firmware/
sprout-sub2api-fork/       separate fork repository, local path services/sub2api_fork/
```

`firmware/` and `services/sub2api_fork/` are ignored by the platform repository
and must be committed in their own repositories. The platform repository does
not keep duplicate source copies.

## Public Endpoints

Public hostnames are maintained only in `deploy/public-endpoints.env`:

| Purpose | Address |
| --- | --- |
| Parent app and device API | `https://api.clarkhub.cn` |
| Voice gateway | `https://voice.clarkhub.cn` |
| AI gateway and Sub2API | `https://sub.clarkhub.cn` |
| Admin console | `https://admin.clarkhub.cn` |

The device MQTT hostname is not confirmed yet. Do not generate production
certificates before it is defined.

## Technology

| Layer | Technology |
| --- | --- |
| Parent app | Flutter, Dart, Riverpod, go_router, Dio, secure storage, json_serializable |
| Admin console | Vue 3, TypeScript, Vite, Pinia, Vue Router, Axios |
| Business services | Go, standard-library HTTP, pgx, go-redis, Eclipse Paho MQTT |
| Realtime voice | Go, WebSocket, Opus, ASR/TTS adapters, `sub2api` |
| Data and messaging | PostgreSQL 16, Redis 7, MQTT/TLS, optional MinIO AIStor |
| Firmware | ESP-IDF, C, FreeRTOS, PlatformIO, ESP32-S3 N16R8 |
| Contracts | JSON Schema, OpenAPI, Dart/TypeScript generated types |
| Observability | `log/slog`, request IDs, trace fields, access logs, redaction |

## Prerequisites

| Tool | Suggested version | Purpose |
| --- | --- | --- |
| Git | Current stable | Version control and external repository bootstrap |
| Flutter | Dart SDK 3.11.5 or newer | Parent app development and tests |
| Node.js | 22.18 or 24.12 or newer | Admin, contracts, and deployment validation |
| Go | 1.27.1 or newer | Both platform services |
| Docker Desktop or Docker Engine | Current stable with Compose v2 | Local dependencies and service deployment |
| PlatformIO CLI | Current stable | Firmware build, flash, and tests |

After checking out the platform repository, run:

```powershell
.\tools\bootstrap.ps1
```

To update local checkouts:

```powershell
.\tools\bootstrap.ps1 -Update
```

`sub2api_fork` keeps `origin` for the editable `sprout-sub2api-fork` repository
and `upstream` for synchronizing upstream only. Never push project business code
to `upstream`.

## Build and Test

### Shared contracts

```powershell
Set-Location tools/contracts
npm ci
npm run validate
npm run generate
```

Generated output goes to `packages/contracts/generated` and
`apps/parent_app/lib/core/contracts/generated`. Generated types must be current
before committing.

### Parent application

```powershell
Set-Location apps/parent_app
flutter pub get
flutter analyze
flutter test
flutter build apk --release
```

Regenerate launcher icons when the Flutter environment is available:

```powershell
dart run flutter_launcher_icons
```

### Admin console

```powershell
Set-Location apps/admin_web
npm ci
npm run type-check
npm run build
npm run test:e2e
```

Install Playwright browsers before the first end-to-end run:

```powershell
npx playwright install
```

Build output is written to `apps/admin_web/dist` and is not committed.

### Go platform services

```powershell
Set-Location services/device_platform
gofmt -w .
go test ./...
go build ./...

Set-Location ../voice_gateway
gofmt -w .
go test ./...
go build ./...
```

Both services currently expose:

```text
GET /healthz
GET /readyz
```

`readyz` checks PostgreSQL, Redis, and MQTT in order and returns `503` when any
dependency is unavailable, so a container is not marked traffic-ready while
its database or message transport is down.

`healthz` reports the `version` actually injected into the running binary.
Release images use the repository `VERSION`; local builds without an injected
version report `dev`.

### Firmware

```powershell
Set-Location firmware
platformio run -e esp32-s3-n16r8
```

All firmware build output, images, and test artifacts go to `bgen/`, which is
not committed. `esp32-s3-n16r8-minimal` verifies that disabled optional modules
still produce a valid build.

### Local deployment validation

```powershell
Set-Location tools/local-deployment
npm ci
npm run validate
```

## Docker Deployment

### 1. Prepare local configuration

From the platform root:

```powershell
.\tools\local-deployment\Initialize-LocalEnvironment.ps1
```

This creates `deploy/.env` from `deploy/.env.example` and generates random
database, Redis, administrator, and service secrets. Never commit `deploy/.env`.

Generate development-only mutual MQTT TLS certificates:

```powershell
.\tools\generate-local-mqtt-certs.ps1
```

Certificates are written under `deploy/mosquitto/certs`, which is ignored by
Git. Development certificates must never be used in production.

### 2. Start Compose services

The Compose file is `deploy/docker-compose.yml`:

| Profile | Runtime contents | Command |
| --- | --- | --- |
| Default | PostgreSQL, Redis, MQTT/TLS | `docker compose -f deploy/docker-compose.yml up -d` |
| AI | Default plus `sub2api` | `docker compose -f deploy/docker-compose.yml --profile ai up -d` |
| Services | Default plus both Go services | `docker compose -f deploy/docker-compose.yml --profile services up -d --build` |
| Full | Default, AI, and Go services | `docker compose -f deploy/docker-compose.yml --profile ai --profile services up -d --build` |

Confirm Docker Desktop and Compose are available:

```powershell
docker version
docker compose version
```

### 3. Check status

```powershell
docker compose -f deploy/docker-compose.yml ps
docker compose -f deploy/docker-compose.yml logs -f device_platform
docker compose -f deploy/docker-compose.yml logs -f voice_gateway
docker compose -f deploy/docker-compose.yml logs -f sub2api
```

Local ports bind to loopback by default:

| Service | Address |
| --- | --- |
| PostgreSQL | `127.0.0.1:5432` |
| Redis | `127.0.0.1:6379` |
| MQTT | `127.0.0.1:1883` |
| MQTT/TLS | `127.0.0.1:8883` |
| `sub2api` | `http://127.0.0.1:8080` |
| `device_platform` | `http://127.0.0.1:8081` |
| `voice_gateway` | `http://127.0.0.1:8082` |

Health checks:

```powershell
Invoke-RestMethod http://127.0.0.1:8081/healthz
Invoke-RestMethod http://127.0.0.1:8081/readyz
Invoke-RestMethod http://127.0.0.1:8082/healthz
Invoke-RestMethod http://127.0.0.1:8082/readyz
```

### 4. Stop and clean up

Stop services while retaining data:

```powershell
docker compose -f deploy/docker-compose.yml down
```

Remove local Compose volumes:

```powershell
docker compose -f deploy/docker-compose.yml down --volumes
```

Removing volumes destroys local PostgreSQL, Redis, MQTT, and `sub2api` data. Run
it only when recovery is not required.

## Optional MinIO AIStor Deployment

Object storage is enabled when compliant short-term media, content packages, or
backups require it. AIStor is intentionally not included in
`deploy/docker-compose.yml`: it needs separate license, certificate, and data
mounts, and its license and capacity planning differ from the business services.

AIStor requires a valid license. A free-tier license can be requested from the
[MinIO pricing page](https://min.io/pricing). The free tier is limited to one
compute resource and does not include commercial support. Review its terms
before production use.

### Linux Docker

```bash
docker pull quay.io/minio/aistor/minio

mkdir -p "$HOME/minio/data" "$HOME/minio/certs"

# Save the license as $HOME/minio/minio.license

docker run -dt \
  -p 9000:9000 -p 9001:9001 \
  -v "$HOME/minio/data:/mnt/data" \
  -v "$HOME/minio/minio.license:/minio.license" \
  -v "$HOME/minio/certs:/etc/minio/certs" \
  --name "aistor-server" \
  quay.io/minio/aistor/minio:latest minio server /mnt/data \
  --license /minio.license

docker logs aistor-server
```

### Windows PowerShell

```powershell
$minioRoot = Join-Path $HOME "minio"
$minioData = Join-Path $minioRoot "data"
$minioCerts = Join-Path $minioRoot "certs"
New-Item -ItemType Directory -Force -Path $minioData, $minioCerts | Out-Null

# Save the license as $minioRoot\minio.license

docker pull quay.io/minio/aistor/minio
docker run -dt `
  -p 9000:9000 -p 9001:9001 `
  -v "${minioData}:/mnt/data" `
  -v "${minioRoot}\minio.license:/minio.license" `
  -v "${minioCerts}:/etc/minio/certs" `
  --name "aistor-server" `
  quay.io/minio/aistor/minio:latest minio server /mnt/data `
  --license /minio.license

docker logs aistor-server
```

### Podman

Prepare directories and the license as above, then replace `docker pull` and
`docker run` with `podman pull` and `podman run`. Container settings, ports, and
volume mounts remain the same.

### Console and security

- The console defaults to `http://localhost:9001`.
- If first-run initialization temporarily uses the local default account,
  change it to a strong password before use. Never carry default credentials
  into a shared or production environment.
- Production must use TLS with trusted certificates, restrict management
  access, and follow the
  [MinIO network encryption guide](https://docs.min.io/aistor/installation/container/network-encryption/).
- Never expose `9000` or `9001` directly to the internet. Cross-host access
  must sit behind a controlled reverse proxy, private network, and access
  policy.
- AIStor is an independent object-storage component. Platform services do not
  yet include AIStor configuration. Integration requires secret injection, TLS
  trust, tenant isolation, retention, and deletion controls first.

### `mc` client

Linux AMD64:

```bash
curl --progress-bar -L \
  https://dl.min.io/aistor/mc/release/linux-amd64/mc -o mc
chmod +x ./mc
sudo mv ./mc /usr/local/bin/
mc --version
```

Linux ARM64:

```bash
curl --progress-bar -L \
  https://dl.min.io/aistor/mc/release/linux-arm64/mc -o mc
chmod +x ./mc
sudo mv ./mc /usr/local/bin/
mc --version
```

## Local Service Configuration

Both Go services currently load environment variables. Example configuration
files describe the deployment shape only.

### `device_platform`

```powershell
$env:DEVICE_PLATFORM_HTTP_HOST = "0.0.0.0"
$env:DEVICE_PLATFORM_HTTP_PORT = "8081"
$env:DEVICE_PLATFORM_DATABASE_DSN = "postgres://sprout:<password>@127.0.0.1:5432/sprout_device_platform?sslmode=disable"
$env:DEVICE_PLATFORM_REDIS_ADDRESS = "127.0.0.1:6379"
$env:DEVICE_PLATFORM_REDIS_PASSWORD = "<redis-password>"
$env:DEVICE_PLATFORM_MQTT_BROKER = "tls://127.0.0.1:8883"
$env:DEVICE_PLATFORM_MQTT_CA_FILE = "deploy/mosquitto/certs/ca.crt"
$env:DEVICE_PLATFORM_MQTT_CLIENT_CERTIFICATE_FILE = "deploy/mosquitto/certs/device.crt"
$env:DEVICE_PLATFORM_MQTT_CLIENT_KEY_FILE = "deploy/mosquitto/certs/device.key"
```

### `voice_gateway`

```powershell
$env:VOICE_GATEWAY_HTTP_HOST = "0.0.0.0"
$env:VOICE_GATEWAY_HTTP_PORT = "8082"
$env:VOICE_GATEWAY_REDIS_ADDRESS = "127.0.0.1:6379"
$env:VOICE_GATEWAY_REDIS_PASSWORD = "<redis-password>"
$env:VOICE_GATEWAY_SUB2API_BASE_URL = "http://127.0.0.1:8080"
$env:VOICE_GATEWAY_SUB2API_API_KEY = "<local-api-key>"
```

Never write real credentials into the README, source, images, logs, or frontend
configuration.

## CI and Release

The platform repository contains:

- `.github/workflows/ci.yml`: validates deployment files, contracts, generated
  types, Go services, the Vue admin console, and the Flutter parent app.
- `.github/workflows/release.yml`: watches `VERSION` on `main`, builds the Go
  services, admin assets, Android APK, and contract archive, generates Chinese
  release notes, verifies download and GitHub assets, and only then promotes the
  four matching GHCR images to `latest`.

Release the platform by changing only the root `VERSION` file in `X.Y.Z` format,
then committing and pushing to `main`. The workflow creates and uses the tag.
The firmware and `sprout-sub2api-fork` repositories run their own CI and release
workflows.

The release pipeline uses `SPROUT_RELEASE_UPLOAD_TOKEN` to publish through the
management API. SFTP is optional: when all five `SPROUT_DOWNLOAD_SFTP_*`
secrets are empty, only the management API is used. If any SFTP secret is
configured, all five are required and the workflow validates the private key
and an exact pinned `known_hosts` entry for the configured host and port before
writing release files. A partial configuration fails closed. The workflow
inspects all four versioned images
(`device-platform`, `voice-gateway`, `admin-web`, and `upgrade-worker`) before
moving `latest`.

## Modularity Rules

- Every feature must be independently addable and removable. Removal may affect
  only its registry, composition root, contract consumers, and build switch.
- Firmware hardware features use `CONFIG_FEATURE_<MODULE_NAME>` and exclude
  source and dependencies in CMake.
- Platform modules keep `domain`, `service`, `handler`, and `repository`
  boundaries.
- Cross-project protocols belong in `packages/contracts`; service-local
  transport contracts stay with their service.
- One concept has one canonical name. Device variants use capability names such
  as `camera`, `display`, `touch`, `cellular`, and `battery`, never character or
  form-factor names.
- Public APIs and complex flows need short developer-facing contract comments
  that explain constraints and reasons, not a restatement of the code.

## Mandatory Security and Privacy Controls

- Capabilities start disabled or in the most conservative state. Social,
  camera, microphone upload, and data collection require explicit guardian
  activation.
- Public network traffic uses TLS 1.2+ with certificate validation. Skipping
  certificate checks is prohibited.
- Every device has a unique identity and short-lived token. Provider and
  service keys exist only in server-side secret management.
- Logs, metrics, audits, and error responses must not contain complete
  conversations, audio, images, tokens, keys, or child personal information.
- AI input and output pass content safety policy. Provider keys never reach the
  device or frontend.
- Audio, images, conversations, and diagnostics use the shortest practical
  retention period and support consent withdrawal, deletion, and account
  deletion.
- Production firmware enables Secure Boot, flash encryption, signed OTA, and
  watchdogs, and disables debug interfaces.

## Git Workflow

Commit messages use:

```text
<type>(<scope>): <summary>
```

Supported types:

```text
feat
fix
refactor
perf
test
docs
build
chore
```

Examples:

```text
feat(device_platform): add device activation flow
fix(voice_gateway): reject expired websocket sessions
docs(workspace): update external repository revisions
```

The platform, firmware, and `sub2api` fork are committed and pushed in their own
repositories. Never commit `AGENTS.md`, planning documents, build output, local
configuration, secrets, certificates, or unrelated files.

## Open Source Components

Third-party components retain their own copyright and license terms. Key
dependencies:

- [Go](https://go.dev/LICENSE): BSD-3-Clause.
- [Flutter and Dart](https://github.com/flutter/flutter/blob/master/LICENSE): BSD-3-Clause.
- [Vue 3](https://github.com/vuejs/core/blob/main/LICENSE): MIT.
- [Vite](https://github.com/vitejs/vite/blob/main/LICENSE): MIT.
- [Pinia](https://github.com/vuejs/pinia/blob/v3/LICENSE): MIT.
- [Vue Router](https://github.com/vuejs/router/blob/main/LICENSE): MIT.
- [Axios](https://github.com/axios/axios/blob/v1.x/LICENSE): MIT.
- [Dio](https://github.com/cfug/dio/blob/main/dio/LICENSE): MIT.
- [Riverpod](https://github.com/rrousselGit/riverpod/blob/master/LICENSE): MIT.
- [go_router](https://github.com/flutter/packages/blob/main/packages/go_router/LICENSE): BSD-3-Clause.
- [PostgreSQL](https://www.postgresql.org/about/licence/): PostgreSQL License.
- [Redis](https://github.com/redis/redis/blob/unstable/LICENSE.txt): AGPL-3.0 or RSALv2/SSPLv1 depending on distribution.
- [Eclipse Mosquitto](https://github.com/eclipse-mosquitto/mosquitto/blob/master/LICENSE.txt): EPL-2.0.
- [ESP-IDF](https://github.com/espressif/esp-idf/blob/master/LICENSE): Apache-2.0.
- [FreeRTOS Kernel](https://github.com/FreeRTOS/FreeRTOS-Kernel/blob/main/LICENSE.md): MIT.
- [PlatformIO Core](https://github.com/platformio/platformio-core/blob/develop/LICENSE): Apache-2.0.
- [MinIO AIStor](https://min.io/pricing): review its commercial license, free-tier limits, and support terms before use.

The platform repository does not yet declare a project-level license. No
license is granted for first-party code until a `LICENSE` file is added.
