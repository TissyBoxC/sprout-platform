/// One age band the catalog is grouped by, matching the child profile tiers.
enum EnglishAgeTier {
  age3to4('age_3_4', '3-4 岁'),
  age5to6('age_5_6', '5-6 岁'),
  age7to8('age_7_8', '7-8 岁');

  const EnglishAgeTier(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static EnglishAgeTier? fromApiValue(String value) {
    for (final tier in EnglishAgeTier.values) {
      if (tier.apiValue == value) {
        return tier;
      }
    }
    return null;
  }
}

/// Whether a practice item is a single word or a spoken sentence pattern.
enum EnglishItemType {
  word('word', '单词'),
  sentence('sentence', '句型');

  const EnglishItemType(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static EnglishItemType? fromApiValue(String value) {
    for (final type in EnglishItemType.values) {
      if (type.apiValue == value) {
        return type;
      }
    }
    return null;
  }
}

/// One vocabulary item inside a lesson.
class EnglishWord {
  const EnglishWord({
    required this.id,
    required this.position,
    required this.term,
    required this.phonetic,
    required this.translation,
    required this.example,
  });

  final String id;
  final int position;
  final String term;
  final String phonetic;
  final String translation;
  final String example;

  factory EnglishWord.fromJson(Map<String, Object?> json) {
    return EnglishWord(
      id: _string(json['id']),
      position: _int(json['position']),
      term: _string(json['term']),
      phonetic: _string(json['phonetic']),
      translation: _string(json['translation']),
      example: _string(json['example']),
    );
  }
}

/// One spoken sentence pattern inside a lesson.
class EnglishSentence {
  const EnglishSentence({
    required this.id,
    required this.position,
    required this.pattern,
    required this.prompt,
    required this.modelAnswer,
  });

  final String id;
  final int position;
  final String pattern;
  final String prompt;
  final String modelAnswer;

  factory EnglishSentence.fromJson(Map<String, Object?> json) {
    return EnglishSentence(
      id: _string(json['id']),
      position: _int(json['position']),
      pattern: _string(json['pattern']),
      prompt: _string(json['prompt']),
      modelAnswer: _string(json['model_answer']),
    );
  }
}

/// A catalog lesson summary as shown in the lesson list.
class EnglishLessonSummary {
  const EnglishLessonSummary({
    required this.lessonId,
    required this.ageTier,
    required this.title,
    required this.summary,
    required this.sequence,
    required this.wordCount,
    required this.sentenceCount,
  });

  final String lessonId;
  final String ageTier;
  final String title;
  final String summary;
  final int sequence;
  final int wordCount;
  final int sentenceCount;

  factory EnglishLessonSummary.fromJson(Map<String, Object?> json) {
    return EnglishLessonSummary(
      lessonId: _string(json['lesson_id']),
      ageTier: _string(json['age_tier']),
      title: _string(json['title']),
      summary: _string(json['summary']),
      sequence: _int(json['sequence']),
      wordCount: _int(json['word_count']),
      sentenceCount: _int(json['sentence_count']),
    );
  }
}

/// A full lesson with its ordered words and sentence patterns.
class EnglishLesson {
  const EnglishLesson({
    required this.lessonId,
    required this.ageTier,
    required this.title,
    required this.summary,
    required this.sequence,
    required this.words,
    required this.sentences,
  });

  final String lessonId;
  final String ageTier;
  final String title;
  final String summary;
  final int sequence;
  final List<EnglishWord> words;
  final List<EnglishSentence> sentences;

  factory EnglishLesson.fromJson(Map<String, Object?> json) {
    return EnglishLesson(
      lessonId: _string(json['lesson_id']),
      ageTier: _string(json['age_tier']),
      title: _string(json['title']),
      summary: _string(json['summary']),
      sequence: _int(json['sequence']),
      words: _list(json['words'], EnglishWord.fromJson),
      sentences: _list(json['sentences'], EnglishSentence.fromJson),
    );
  }
}

/// One stored pronunciation-feedback attempt.
class EnglishPracticeAttempt {
  const EnglishPracticeAttempt({
    required this.id,
    required this.childId,
    required this.lessonId,
    required this.itemId,
    required this.itemType,
    required this.targetText,
    required this.recognizedText,
    required this.score,
    required this.passed,
    required this.practicedAt,
  });

  final String id;
  final String childId;
  final String lessonId;
  final String itemId;
  final EnglishItemType itemType;
  final String targetText;
  final String recognizedText;
  final int score;
  final bool passed;
  final DateTime practicedAt;

  factory EnglishPracticeAttempt.fromJson(Map<String, Object?> json) {
    final itemType = EnglishItemType.fromApiValue(_string(json['item_type']));
    if (itemType == null) {
      throw const FormatException('unknown english item type');
    }
    return EnglishPracticeAttempt(
      id: _string(json['id']),
      childId: _string(json['child_id']),
      lessonId: _string(json['lesson_id']),
      itemId: _string(json['item_id']),
      itemType: itemType,
      targetText: _string(json['target_text']),
      recognizedText: _string(json['recognized_text']),
      score: _int(json['score']),
      passed: json['passed'] == true,
      practicedAt: _dateTime(json['practiced_at']),
    );
  }
}

/// Guardian-visible best score rollup for one practiced item.
class EnglishItemProgress {
  const EnglishItemProgress({
    required this.lessonId,
    required this.itemId,
    required this.itemType,
    required this.targetText,
    required this.bestScore,
    required this.attempts,
    required this.passed,
    required this.lastAt,
  });

  final String lessonId;
  final String itemId;
  final EnglishItemType itemType;
  final String targetText;
  final int bestScore;
  final int attempts;
  final bool passed;
  final DateTime lastAt;

  factory EnglishItemProgress.fromJson(Map<String, Object?> json) {
    final itemType =
        EnglishItemType.fromApiValue(_string(json['item_type'])) ??
        EnglishItemType.word;
    return EnglishItemProgress(
      lessonId: _string(json['lesson_id']),
      itemId: _string(json['item_id']),
      itemType: itemType,
      targetText: _string(json['target_text']),
      bestScore: _int(json['best_score']),
      attempts: _int(json['attempts']),
      passed: json['passed'] == true,
      lastAt: _dateTime(json['last_at']),
    );
  }
}

/// Guardian-visible lesson completion rollup.
class EnglishLessonProgress {
  const EnglishLessonProgress({
    required this.lessonId,
    required this.attempts,
    required this.passedItems,
    required this.totalItems,
    required this.bestScore,
    required this.averageScore,
    required this.lastPracticed,
  });

  final String lessonId;
  final int attempts;
  final int passedItems;
  final int totalItems;
  final int bestScore;
  final int averageScore;
  final DateTime? lastPracticed;

  bool get isComplete => totalItems > 0 && passedItems >= totalItems;

  /// Completion ratio in the range 0-1, safe when the lesson has no items.
  double get completion {
    if (totalItems <= 0) {
      return 0;
    }
    return (passedItems / totalItems).clamp(0, 1).toDouble();
  }

  factory EnglishLessonProgress.fromJson(Map<String, Object?> json) {
    return EnglishLessonProgress(
      lessonId: _string(json['lesson_id']),
      attempts: _int(json['attempts']),
      passedItems: _int(json['passed_items']),
      totalItems: _int(json['total_items']),
      bestScore: _int(json['best_score']),
      averageScore: _int(json['average_score']),
      lastPracticed: _optionalDateTime(json['last_practiced']),
    );
  }
}

/// The progress response: lesson rollups plus per-item best scores.
class EnglishProgress {
  const EnglishProgress({required this.lessons, required this.items});

  const EnglishProgress.empty() : lessons = const [], items = const [];

  final List<EnglishLessonProgress> lessons;
  final List<EnglishItemProgress> items;

  bool get isEmpty => lessons.isEmpty && items.isEmpty;

  EnglishLessonProgress? forLesson(String lessonId) {
    for (final lesson in lessons) {
      if (lesson.lessonId == lessonId) {
        return lesson;
      }
    }
    return null;
  }

  List<EnglishItemProgress> itemsForLesson(String lessonId) {
    return [
      for (final item in items)
        if (item.lessonId == lessonId) item,
    ];
  }
}

String _string(Object? value) => value is String ? value : '';

int _int(Object? value) {
  if (value is num) {
    return value.toInt();
  }
  if (value is String) {
    return int.tryParse(value) ?? 0;
  }
  return 0;
}

DateTime _dateTime(Object? value) {
  return _optionalDateTime(value) ?? DateTime.fromMillisecondsSinceEpoch(0);
}

DateTime? _optionalDateTime(Object? value) {
  if (value is String && value.isNotEmpty) {
    return DateTime.tryParse(value)?.toLocal();
  }
  return null;
}

List<T> _list<T>(
  Object? value,
  T Function(Map<String, Object?>) convert,
) {
  if (value is! List) {
    return const [];
  }
  return value
      .whereType<Map>()
      .map((item) => convert(Map<String, Object?>.from(item)))
      .toList(growable: false);
}
