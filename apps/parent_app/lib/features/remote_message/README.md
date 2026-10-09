# Remote Message Feature

The guardian-authored device message flow is implemented in the
`features/notification` module:

- `notification/domain/notification.dart` — inbox contracts and labels.
- `notification/data/notification_api.dart` — guardian inbox, read state, and
  `family-messages` delivery.
- `notification/application/notification_controller.dart` — paged inbox state
  and unread badge.
- `notification/presentation/family_message_page.dart` — device picker,
  message body, display toggle, and display duration.

This directory is kept as the historical home of the feature name. Voice
messages, scheduled playback, and per-recipient delivery status are not part of
the current contract and must not be added here without a platform-side
contract and moderation path.
