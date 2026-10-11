import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/english_controller.dart';
import '../domain/english.dart';

/// Guardian view of the English learning catalog and the child's practice
/// progress.
class EnglishPage extends ConsumerWidget {
  const EnglishPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(englishControllerProvider);
    final children = ref.watch(childProfileControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('英语启蒙'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (value) =>
              value.catalogEmpty ? 'english-empty' : 'english-content',
          error: (_, _) => 'english-error',
          loading: () => 'english-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('english-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _EnglishErrorState(
            key: const ValueKey<String>('english-error'),
            message: EnglishController.messageFor(error),
            onRetry: () => ref.read(englishControllerProvider.notifier).refresh(),
          ),
          data: (value) => _EnglishContent(
            key: ValueKey<String>(
              value.catalogEmpty ? 'english-empty' : 'english-content',
            ),
            state: value,
            children: children.value ?? const <ChildProfile>[],
          ),
        ),
      ),
    );
  }
}

void _leave(BuildContext context) {
  if (context.canPop()) {
    context.pop();
  } else {
    context.go('/home');
  }
}

class _EnglishContent extends ConsumerWidget {
  const _EnglishContent({
    required this.state,
    required this.children,
    super.key,
  });

  final EnglishLearningState state;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(englishControllerProvider.notifier);
    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(
            child: _ProgressSummary(state: state, children: children),
          ),
          const SizedBox(height: 12),
          AppReveal(
            child: _AgeTierSelector(
              selected: state.ageTier,
              onSelected: controller.selectAgeTier,
            ),
          ),
          const SizedBox(height: 16),
          if (state.catalogEmpty)
            const AppReveal(
              child: _EmptyCard(
                icon: Icons.menu_book_outlined,
                message: '这个年龄段的课程正在准备中，先看看其他年龄段吧。',
              ),
            )
          else
            for (final lesson in state.lessons) ...[
              AppReveal(
                child: _LessonTile(
                  lesson: lesson,
                  progress: state.progressFor(lesson.lessonId),
                ),
              ),
              const SizedBox(height: 10),
            ],
        ],
      ),
    );
  }
}

class _ProgressSummary extends ConsumerWidget {
  const _ProgressSummary({required this.state, required this.children});

  final EnglishLearningState state;
  final List<ChildProfile> children;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(englishControllerProvider.notifier);
    final totalItems = state.progress.lessons.fold<int>(
      0,
      (sum, lesson) => sum + lesson.totalItems,
    );
    final passedItems = state.progress.lessons.fold<int>(
      0,
      (sum, lesson) => sum + lesson.passedItems,
    );
    final childIds = [
      for (final child in children)
        if (child.childId.isNotEmpty) child.childId,
    ];
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.record_voice_over_rounded),
                const SizedBox(width: 10),
                Text('练习进度', style: Theme.of(context).textTheme.titleMedium),
                const Spacer(),
                Text(
                  '$passedItems / $totalItems 项已通过',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              '跟着课程练单词和句型，设备会给出发音评分。',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            if (childIds.isNotEmpty) ...[
              const SizedBox(height: 12),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  FilterChip(
                    label: const Text('全部'),
                    selected: state.childFilter == null,
                    onSelected: (_) => controller.filterByChild(null),
                  ),
                  for (final childId in childIds)
                    FilterChip(
                      label: Text(_childName(children, childId)),
                      selected: state.childFilter == childId,
                      onSelected: (_) => controller.filterByChild(childId),
                    ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _AgeTierSelector extends StatelessWidget {
  const _AgeTierSelector({required this.selected, required this.onSelected});

  final EnglishAgeTier? selected;
  final ValueChanged<EnglishAgeTier?> onSelected;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        ChoiceChip(
          label: const Text('全部年龄'),
          selected: selected == null,
          onSelected: (_) => onSelected(null),
        ),
        for (final tier in EnglishAgeTier.values)
          ChoiceChip(
            label: Text(tier.label),
            selected: selected == tier,
            onSelected: (_) => onSelected(tier),
          ),
      ],
    );
  }
}

class _LessonTile extends StatelessWidget {
  const _LessonTile({required this.lesson, required this.progress});

  final EnglishLessonSummary lesson;
  final EnglishLessonProgress? progress;

  @override
  Widget build(BuildContext context) {
    final tier = EnglishAgeTier.fromApiValue(lesson.ageTier);
    final completion = progress?.completion ?? 0;
    return Card(
      child: InkWell(
        borderRadius: BorderRadius.circular(8),
        onTap: () => Navigator.of(context).push<void>(
          MaterialPageRoute<void>(
            builder: (context) => EnglishLessonPage(lessonId: lesson.lessonId),
          ),
        ),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      lesson.title,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                  ),
                  if (progress?.isComplete == true)
                    const Icon(Icons.verified_rounded, size: 20),
                ],
              ),
              if (lesson.summary.isNotEmpty) ...[
                const SizedBox(height: 6),
                Text(
                  lesson.summary,
                  style: Theme.of(context).textTheme.bodyMedium,
                ),
              ],
              const SizedBox(height: 10),
              Text(
                '${tier?.label ?? lesson.ageTier} · ${lesson.wordCount} 个单词 · ${lesson.sentenceCount} 个句型',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 10),
              LinearProgressIndicator(value: completion),
              const SizedBox(height: 6),
              Text(
                progress == null
                    ? '还没有练习记录'
                    : '已通过 ${progress!.passedItems}/${progress!.totalItems} 项 · 最高 ${progress!.bestScore} 分',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Lesson detail: the word list, the sentence patterns, and the child's best
/// scores per item.
class EnglishLessonPage extends ConsumerStatefulWidget {
  const EnglishLessonPage({required this.lessonId, super.key});

  final String lessonId;

  @override
  ConsumerState<EnglishLessonPage> createState() => _EnglishLessonPageState();
}

class _EnglishLessonPageState extends ConsumerState<EnglishLessonPage> {
  late Future<EnglishLesson> _lessonFuture;

  @override
  void initState() {
    super.initState();
    _lessonFuture = ref
        .read(englishControllerProvider.notifier)
        .loadLesson(widget.lessonId);
  }

  void _reload() {
    setState(() {
      _lessonFuture = ref
          .read(englishControllerProvider.notifier)
          .loadLesson(widget.lessonId);
    });
  }

  @override
  Widget build(BuildContext context) {
    final progressState = ref.watch(englishControllerProvider);
    final progress = progressState.value?.progress.forLesson(widget.lessonId);
    final itemProgress =
        progressState.value?.progress.itemsForLesson(widget.lessonId) ??
            const <EnglishItemProgress>[];
    return Scaffold(
      appBar: AppBar(title: const Text('课程详情')),
      body: FutureBuilder<EnglishLesson>(
        future: _lessonFuture,
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const Center(child: CircularProgressIndicator());
          }
          if (snapshot.hasError || !snapshot.hasData) {
            return _EnglishErrorState(
              message: EnglishController.messageFor(
                snapshot.error ?? Exception('missing lesson'),
              ),
              onRetry: () async => _reload(),
            );
          }
          final lesson = snapshot.data!;
          return ListView(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
            children: [
              Card(
                child: Padding(
                  padding: const EdgeInsets.all(20),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        lesson.title,
                        style: Theme.of(context).textTheme.titleLarge,
                      ),
                      if (lesson.summary.isNotEmpty) ...[
                        const SizedBox(height: 8),
                        Text(lesson.summary),
                      ],
                      if (progress != null) ...[
                        const SizedBox(height: 16),
                        LinearProgressIndicator(value: progress.completion),
                        const SizedBox(height: 8),
                        Text(
                          '已通过 ${progress.passedItems}/${progress.totalItems} 项 · 平均 ${progress.averageScore} 分',
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                      ],
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 16),
              Text('单词', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              if (lesson.words.isEmpty)
                const _EmptyCard(
                  icon: Icons.abc_rounded,
                  message: '这节课还没有单词。',
                )
              else
                for (final word in lesson.words)
                  _WordTile(
                    word: word,
                    progress: _itemProgress(itemProgress, word.id),
                  ),
              const SizedBox(height: 24),
              Text('句型练习', style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              if (lesson.sentences.isEmpty)
                const _EmptyCard(
                  icon: Icons.chat_bubble_outline_rounded,
                  message: '这节课还没有句型练习。',
                )
              else
                for (final sentence in lesson.sentences)
                  _SentenceTile(
                    sentence: sentence,
                    progress: _itemProgress(itemProgress, sentence.id),
                  ),
            ],
          );
        },
      ),
    );
  }
}

class _WordTile extends StatelessWidget {
  const _WordTile({required this.word, required this.progress});

  final EnglishWord word;
  final EnglishItemProgress? progress;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        isThreeLine: word.example.isNotEmpty,
        title: Row(
          children: [
            Text(
              word.term,
              style: Theme.of(context).textTheme.titleMedium,
            ),
            if (word.phonetic.isNotEmpty) ...[
              const SizedBox(width: 8),
              Text(
                '/${word.phonetic}/',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ],
        ),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (word.translation.isNotEmpty) Text(word.translation),
            if (word.example.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(
                word.example,
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ],
        ),
        trailing: _ScoreBadge(progress: progress),
      ),
    );
  }
}

class _SentenceTile extends StatelessWidget {
  const _SentenceTile({required this.sentence, required this.progress});

  final EnglishSentence sentence;
  final EnglishItemProgress? progress;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        isThreeLine: sentence.prompt.isNotEmpty,
        title: Text(sentence.pattern),
        subtitle: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (sentence.prompt.isNotEmpty) Text(sentence.prompt),
            if (sentence.modelAnswer.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(
                '参考：${sentence.modelAnswer}',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ],
        ),
        trailing: _ScoreBadge(progress: progress),
      ),
    );
  }
}

class _ScoreBadge extends StatelessWidget {
  const _ScoreBadge({required this.progress});

  final EnglishItemProgress? progress;

  @override
  Widget build(BuildContext context) {
    final value = progress;
    if (value == null) {
      return const Icon(Icons.mic_none_rounded);
    }
    final colorScheme = Theme.of(context).colorScheme;
    final color = value.passed ? colorScheme.primary : colorScheme.outline;
    return Column(
      mainAxisAlignment: MainAxisAlignment.center,
      crossAxisAlignment: CrossAxisAlignment.end,
      children: [
        Icon(
          value.passed ? Icons.mic_rounded : Icons.mic_none_rounded,
          color: color,
        ),
        const SizedBox(height: 2),
        Text(
          '${value.bestScore}',
          style: Theme.of(context).textTheme.bodySmall?.copyWith(color: color),
        ),
      ],
    );
  }
}

class _EmptyCard extends StatelessWidget {
  const _EmptyCard({required this.icon, required this.message});

  final IconData icon;
  final String message;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Row(
          children: [
            Icon(icon, color: Theme.of(context).colorScheme.primary),
            const SizedBox(width: 12),
            Expanded(child: Text(message)),
          ],
        ),
      ),
    );
  }
}

class _EnglishErrorState extends StatelessWidget {
  const _EnglishErrorState({
    required this.message,
    required this.onRetry,
    super.key,
  });

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 12),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

EnglishItemProgress? _itemProgress(
  List<EnglishItemProgress> items,
  String itemId,
) {
  for (final item in items) {
    if (item.itemId == itemId) {
      return item;
    }
  }
  return null;
}

String _childName(List<ChildProfile> children, String childId) {
  for (final child in children) {
    if (child.childId == childId) {
      return child.nickname;
    }
  }
  return '宝贝';
}
