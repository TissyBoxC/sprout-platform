import 'package:flutter_riverpod/flutter_riverpod.dart';

/// The clock a screen reads "now" from.
///
/// It is a provider rather than a call to `DateTime.now()` at the point of use
/// so a relative time can be rendered predictably in tests without changing
/// what production does.
final currentTimeProvider = Provider<DateTime Function()>((ref) {
  return () => DateTime.now().toUtc();
});
