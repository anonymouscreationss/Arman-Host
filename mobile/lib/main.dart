import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

const _gold = Color(0xFFF5C15A);
const _navy = Color(0xFF0D1321);
const _panel = Color(0xFF17243A);
const _muted = Color(0xFF9AA8BC);
const _apiBaseUrl = String.fromEnvironment('API_BASE_URL', defaultValue: '');

void main() {
  runApp(const ArmanApp());
}

class ArmanApp extends StatelessWidget {
  const ArmanApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'ARMAN',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        brightness: Brightness.dark,
        scaffoldBackgroundColor: _navy,
        colorScheme: ColorScheme.fromSeed(
          seedColor: _gold,
          brightness: Brightness.dark,
          surface: _panel,
        ),
        fontFamily: 'Poppins',
        inputDecorationTheme: InputDecorationTheme(
          filled: true,
          fillColor: Colors.white.withValues(alpha: .06),
          border: OutlineInputBorder(
            borderRadius: BorderRadius.circular(12),
            borderSide: BorderSide(color: Colors.white.withValues(alpha: .12)),
          ),
          enabledBorder: OutlineInputBorder(
            borderRadius: BorderRadius.circular(12),
            borderSide: BorderSide(color: Colors.white.withValues(alpha: .12)),
          ),
        ),
      ),
      home: const BootstrapScreen(),
    );
  }
}

class Session {
  Session(this.preferences);

  final SharedPreferences preferences;
  String? accessToken;
  String? refreshToken;
  Map<String, dynamic>? user;

  Future<void> restore() async {
    accessToken = preferences.getString('arman.accessToken');
    refreshToken = preferences.getString('arman.refreshToken');
    final rawUser = preferences.getString('arman.user');
    if (rawUser != null) {
      user = jsonDecode(rawUser) as Map<String, dynamic>;
    }
  }

  Future<void> save(Map<String, dynamic> data) async {
    accessToken = data['accessToken'] as String?;
    refreshToken = data['refreshToken'] as String?;
    user = (data['user'] as Map?)?.cast<String, dynamic>();
    await preferences.setString('arman.accessToken', accessToken ?? '');
    await preferences.setString('arman.refreshToken', refreshToken ?? '');
    await preferences.setString('arman.user', jsonEncode(user));
  }

  Future<void> clear() async {
    accessToken = null;
    refreshToken = null;
    user = null;
    await preferences.remove('arman.accessToken');
    await preferences.remove('arman.refreshToken');
    await preferences.remove('arman.user');
  }
}

class ArmanApi {
  ArmanApi(this.session);

  final Session session;

  Uri _uri(String path) {
    if (_apiBaseUrl.isEmpty) {
      throw StateError(
        'The mobile API URL is not configured. Launch with '
        '--dart-define=API_BASE_URL=https://your-api.example.com.',
      );
    }
    return Uri.parse('$_apiBaseUrl$path');
  }

  Future<Map<String, dynamic>> _request(
    String path, {
    String method = 'GET',
    Map<String, dynamic>? body,
  }) async {
    final headers = <String, String>{'Content-Type': 'application/json'};
    if (session.accessToken != null && session.accessToken!.isNotEmpty) {
      headers['Authorization'] = 'Bearer ${session.accessToken}';
    }
    final uri = _uri(path);
    final response = switch (method) {
      'POST' => await http.post(uri, headers: headers, body: jsonEncode(body)),
      'PATCH' => await http.patch(uri, headers: headers, body: jsonEncode(body)),
      'DELETE' => await http.delete(uri, headers: headers),
      _ => await http.get(uri, headers: headers),
    };
    if (response.statusCode == 204) {
      return {'success': true};
    }
    final decoded = jsonDecode(response.body) as Map<String, dynamic>;
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw ApiException(
        decoded['message']?.toString() ?? 'The request could not be completed.',
        response.statusCode,
      );
    }
    return decoded;
  }

  Future<Map<String, dynamic>> signIn(String email, String password) =>
      _request('/api/v1/auth/login', method: 'POST', body: {
        'email': email,
        'password': password,
      });

  Future<Map<String, dynamic>> register(
    String name,
    String username,
    String email,
    String password,
  ) =>
      _request('/api/v1/auth/register', method: 'POST', body: {
        'name': name,
        'username': username,
        'email': email,
        'password': password,
      });

  Future<List<Map<String, dynamic>>> resources([String query = '']) async {
    final suffix = query.trim().isEmpty
        ? ''
        : '&q=${Uri.encodeQueryComponent(query.trim())}';
    final response = await _request('/api/v1/resources?limit=20$suffix');
    return ((response['data'] as Map?)?['items'] as List? ?? [])
        .map((item) => (item as Map).cast<String, dynamic>())
        .toList();
  }

  Future<List<Map<String, dynamic>>> bookmarks() async {
    final response = await _request('/api/v1/bookmarks');
    return (response['data'] as List? ?? [])
        .map((item) => (item as Map).cast<String, dynamic>())
        .toList();
  }

  Future<Map<String, dynamic>> profile() async {
    final response = await _request('/api/v1/profiles/me');
    return (response['data'] as Map).cast<String, dynamic>();
  }

  Future<Map<String, dynamic>> updatePrivacy(
    Map<String, dynamic> settings,
  ) async {
    final response =
        await _request('/api/v1/profiles/me', method: 'PATCH', body: settings);
    return (response['data'] as Map).cast<String, dynamic>();
  }

  Future<void> setBookmark(String resourceId, bool save) async {
    await _request(
      '/api/v1/resources/$resourceId/bookmark',
      method: save ? 'POST' : 'DELETE',
    );
  }
}

class ApiException implements Exception {
  ApiException(this.message, this.statusCode);

  final String message;
  final int statusCode;

  @override
  String toString() => message;
}

class BootstrapScreen extends StatefulWidget {
  const BootstrapScreen({super.key});

  @override
  State<BootstrapScreen> createState() => _BootstrapScreenState();
}

class _BootstrapScreenState extends State<BootstrapScreen> {
  String? error;

  @override
  void initState() {
    super.initState();
    _bootstrap();
  }

  Future<void> _bootstrap() async {
    try {
      final preferences = await SharedPreferences.getInstance();
      final session = Session(preferences);
      await session.restore();
      if (!mounted) return;
      final onboardingComplete =
          preferences.getBool('arman.onboardingComplete') ?? false;
      Navigator.of(context).pushReplacement(
        MaterialPageRoute<void>(
          builder: (_) => onboardingComplete
              ? ArmanShell(session: session)
              : OnboardingScreen(session: session),
        ),
      );
    } catch (exception) {
      setState(() => error = exception.toString());
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Center(
        child: error == null
            ? const CircularProgressIndicator(color: _gold)
            : Padding(
                padding: const EdgeInsets.all(24),
                child: Text(error!, textAlign: TextAlign.center),
              ),
      ),
    );
  }
}

class OnboardingScreen extends StatefulWidget {
  const OnboardingScreen({required this.session, super.key});

  final Session session;

  @override
  State<OnboardingScreen> createState() => _OnboardingScreenState();
}

class _OnboardingScreenState extends State<OnboardingScreen> {
  final controller = PageController();
  int page = 0;

  Future<void> _finish() async {
    await widget.session.preferences.setBool('arman.onboardingComplete', true);
    if (!mounted) return;
    Navigator.of(context).pushReplacement(
      MaterialPageRoute<void>(
        builder: (_) => ArmanShell(session: widget.session),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final headings = [
      'A calmer way to learn.',
      'Keep every next step visible.',
      'Your journey starts here.',
    ];
    final descriptions = [
      'Resources, practice, and progress designed around you.',
      'Save useful material, build momentum, and return when you are ready.',
      'Create an account when you want persistence across your study sessions.',
    ];
    return Scaffold(
      body: SafeArea(
        child: PageView.builder(
          controller: controller,
          onPageChanged: (value) => setState(() => page = value),
          itemCount: headings.length,
          itemBuilder: (_, index) => Padding(
            padding: const EdgeInsets.fromLTRB(28, 54, 28, 28),
            child: Column(
              children: [
                Image.asset('assets/brand/arman-logo.png', height: 220),
                const SizedBox(height: 28),
                Text(
                  'WELCOME TO ARMAN',
                  style: Theme.of(context).textTheme.labelSmall?.copyWith(
                        color: _gold,
                        letterSpacing: 3,
                        fontWeight: FontWeight.w700,
                      ),
                ),
                const SizedBox(height: 16),
                Text(
                  headings[index],
                  textAlign: TextAlign.center,
                  style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                ),
                const SizedBox(height: 14),
                Text(
                  descriptions[index],
                  textAlign: TextAlign.center,
                  style: const TextStyle(color: _muted, height: 1.6),
                ),
                const Spacer(),
                Row(
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: List.generate(
                    headings.length,
                    (dot) => AnimatedContainer(
                      duration: const Duration(milliseconds: 180),
                      margin: const EdgeInsets.symmetric(horizontal: 3),
                      height: 5,
                      width: dot == page ? 22 : 5,
                      decoration: BoxDecoration(
                        color: dot == page ? _gold : _muted,
                        borderRadius: BorderRadius.circular(9),
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 24),
                FilledButton(
                  onPressed: () {
                    if (page == headings.length - 1) {
                      _finish();
                    } else {
                      controller.nextPage(
                        duration: const Duration(milliseconds: 250),
                        curve: Curves.easeOut,
                      );
                    }
                  },
                  style: FilledButton.styleFrom(
                    backgroundColor: _gold,
                    foregroundColor: _navy,
                    minimumSize: const Size.fromHeight(52),
                  ),
                  child: Text(page == headings.length - 1 ? 'Begin' : 'Continue'),
                ),
                TextButton(
                  onPressed: _finish,
                  child: const Text('Skip for now', style: TextStyle(color: _muted)),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class ArmanShell extends StatefulWidget {
  const ArmanShell({required this.session, super.key});

  final Session session;

  @override
  State<ArmanShell> createState() => _ArmanShellState();
}

class _ArmanShellState extends State<ArmanShell> {
  int selected = 0;

  @override
  Widget build(BuildContext context) {
    final api = ArmanApi(widget.session);
    final pages = [
      widget.session.user == null
          ? AuthPage(
              api: api,
              session: widget.session,
              onComplete: () => setState(() {}),
            )
          : HomePage(api: api, user: widget.session.user),
      ExplorePage(api: api),
      widget.session.user == null
          ? AuthPage(
              api: api,
              session: widget.session,
              onComplete: () => setState(() {}),
            )
          : SavedPage(api: api),
      widget.session.user == null
          ? AuthPage(
              api: api,
              session: widget.session,
              onComplete: () => setState(() {}),
            )
          : SettingsPage(api: api, session: widget.session),
    ];
    return Scaffold(
      body: pages[selected],
      bottomNavigationBar: NavigationBar(
        selectedIndex: selected,
        onDestinationSelected: (value) => setState(() => selected = value),
        backgroundColor: _panel,
        indicatorColor: _gold.withValues(alpha: .2),
        destinations: const [
          NavigationDestination(icon: Icon(Icons.home_outlined), selectedIcon: Icon(Icons.home), label: 'Home'),
          NavigationDestination(icon: Icon(Icons.search), label: 'Explore'),
          NavigationDestination(icon: Icon(Icons.bookmark_border), selectedIcon: Icon(Icons.bookmark), label: 'Saved'),
          NavigationDestination(icon: Icon(Icons.tune), label: 'Settings'),
        ],
      ),
    );
  }
}

class AuthPage extends StatefulWidget {
  const AuthPage({
    required this.api,
    required this.session,
    required this.onComplete,
    super.key,
  });

  final ArmanApi api;
  final Session session;
  final VoidCallback onComplete;

  @override
  State<AuthPage> createState() => _AuthPageState();
}

class _AuthPageState extends State<AuthPage> {
  bool registerMode = false;
  bool submitting = false;
  String? error;
  final name = TextEditingController();
  final username = TextEditingController();
  final email = TextEditingController();
  final password = TextEditingController();

  Future<void> _submit() async {
    setState(() {
      submitting = true;
      error = null;
    });
    try {
      final response = registerMode
          ? await widget.api.register(
              name.text.trim(),
              username.text.trim(),
              email.text.trim(),
              password.text,
            )
          : await widget.api.signIn(email.text.trim(), password.text);
      await widget.session.save((response['data'] as Map).cast<String, dynamic>());
      widget.onComplete();
    } catch (exception) {
      setState(() => error = exception.toString());
    } finally {
      if (mounted) setState(() => submitting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 68, 20, 32),
      children: [
        Image.asset('assets/brand/arman-logo.png', height: 150),
        const SizedBox(height: 26),
        Text(
          registerMode ? 'Create your account.' : 'Welcome back.',
          style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                fontWeight: FontWeight.w700,
              ),
        ),
        const SizedBox(height: 8),
        const Text(
          'Sign in to save resources and keep your learning journey across sessions.',
          style: TextStyle(color: _muted, height: 1.5),
        ),
        const SizedBox(height: 26),
        if (registerMode) ...[
          TextField(controller: name, decoration: const InputDecoration(labelText: 'Name')),
          const SizedBox(height: 12),
          TextField(controller: username, decoration: const InputDecoration(labelText: 'Username')),
          const SizedBox(height: 12),
        ],
        TextField(
          controller: email,
          keyboardType: TextInputType.emailAddress,
          decoration: const InputDecoration(labelText: 'Email'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: password,
          obscureText: true,
          decoration: const InputDecoration(labelText: 'Password'),
        ),
        if (error != null) ...[
          const SizedBox(height: 12),
          Text(error!, style: const TextStyle(color: Colors.redAccent)),
        ],
        const SizedBox(height: 20),
        FilledButton(
          onPressed: submitting ? null : _submit,
          style: FilledButton.styleFrom(
            backgroundColor: _gold,
            foregroundColor: _navy,
            minimumSize: const Size.fromHeight(52),
          ),
          child: Text(
            submitting ? 'Working…' : registerMode ? 'Create account' : 'Sign in',
          ),
        ),
        TextButton(
          onPressed: submitting
              ? null
              : () => setState(() {
                    registerMode = !registerMode;
                    error = null;
                  }),
          child: Text(
            registerMode ? 'Already have an account? Sign in' : 'New to ARMAN? Create an account',
            style: const TextStyle(color: _gold),
          ),
        ),
      ],
    );
  }
}

class HomePage extends StatelessWidget {
  const HomePage({required this.api, required this.user, super.key});

  final ArmanApi api;
  final Map<String, dynamic>? user;

  @override
  Widget build(BuildContext context) {
    return ResourcePage(
      api: api,
      title: user == null ? 'Good morning.' : 'Good morning, ${user!['name']}.',
      subtitle: 'Keep one useful step in front of you.',
    );
  }
}

class ExplorePage extends StatefulWidget {
  const ExplorePage({required this.api, super.key});

  final ArmanApi api;

  @override
  State<ExplorePage> createState() => _ExplorePageState();
}

class _ExplorePageState extends State<ExplorePage> {
  final search = TextEditingController();
  String query = '';

  @override
  Widget build(BuildContext context) {
    return ResourcePage(
      api: widget.api,
      title: 'Explore',
      subtitle: 'Search notes, subjects, and courses.',
      query: query,
      header: TextField(
        controller: search,
        textInputAction: TextInputAction.search,
        onSubmitted: (value) => setState(() => query = value),
        decoration: InputDecoration(
          hintText: 'Search resources',
          suffixIcon: IconButton(
            onPressed: () => setState(() => query = search.text),
            icon: const Icon(Icons.search),
          ),
        ),
      ),
    );
  }
}

class SavedPage extends StatelessWidget {
  const SavedPage({required this.api, super.key});

  final ArmanApi api;

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<List<Map<String, dynamic>>>(
      future: api.bookmarks(),
      builder: (context, snapshot) {
        if (snapshot.hasError) {
          return ErrorState(message: snapshot.error.toString());
        }
        if (!snapshot.hasData) {
          return const LoadingState();
        }
        return ResourceList(
          api: api,
          resources: snapshot.data!,
          title: 'My library',
          subtitle: 'Keep what helps you grow.',
          initiallySaved: snapshot.data!,
        );
      },
    );
  }
}

class ResourcePage extends StatelessWidget {
  const ResourcePage({
    required this.api,
    required this.title,
    required this.subtitle,
    this.user,
    this.query = '',
    this.header,
    super.key,
  });

  final ArmanApi api;
  final String title;
  final String subtitle;
  final Map<String, dynamic>? user;
  final String query;
  final Widget? header;

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<List<Map<String, dynamic>>>(
      future: api.resources(query),
      builder: (context, snapshot) {
        if (snapshot.hasError) {
          return ErrorState(message: snapshot.error.toString());
        }
        if (!snapshot.hasData) return const LoadingState();
        return ResourceList(
          api: api,
          resources: snapshot.data!,
          title: title,
          subtitle: subtitle,
          header: header,
        );
      },
    );
  }
}

class ResourceList extends StatefulWidget {
  const ResourceList({
    required this.api,
    required this.resources,
    required this.title,
    required this.subtitle,
    this.initiallySaved,
    this.header,
    super.key,
  });

  final ArmanApi api;
  final List<Map<String, dynamic>> resources;
  final List<Map<String, dynamic>>? initiallySaved;
  final String title;
  final String subtitle;
  final Widget? header;

  @override
  State<ResourceList> createState() => _ResourceListState();
}

class _ResourceListState extends State<ResourceList> {
  late final Set<String> saved = {
    ...(widget.initiallySaved ?? []).map((item) => item['id'].toString()),
  };

  Future<void> _toggle(Map<String, dynamic> resource) async {
    final id = resource['id'].toString();
    final next = !saved.contains(id);
    try {
      await widget.api.setBookmark(id, next);
      setState(() => next ? saved.add(id) : saved.remove(id));
    } catch (exception) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(exception.toString())),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    return CustomScrollView(
      slivers: [
        SliverPadding(
          padding: const EdgeInsets.fromLTRB(20, 56, 20, 20),
          sliver: SliverToBoxAdapter(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(widget.title, style: Theme.of(context).textTheme.headlineMedium?.copyWith(fontWeight: FontWeight.w700)),
                const SizedBox(height: 6),
                Text(widget.subtitle, style: const TextStyle(color: _muted)),
                if (widget.header != null) ...[
                  const SizedBox(height: 22),
                  widget.header!,
                ],
                const SizedBox(height: 24),
              ],
            ),
          ),
        ),
        if (widget.resources.isEmpty)
          const SliverFillRemaining(
            hasScrollBody: false,
            child: EmptyState(),
          )
        else
          SliverPadding(
            padding: const EdgeInsets.symmetric(horizontal: 20),
            sliver: SliverList.builder(
              itemCount: widget.resources.length,
              itemBuilder: (_, index) {
                final resource = widget.resources[index];
                final id = resource['id'].toString();
                return Card(
                  color: Colors.white.withValues(alpha: .055),
                  margin: const EdgeInsets.only(bottom: 12),
                  child: ListTile(
                    contentPadding: const EdgeInsets.all(16),
                    onTap: () => showModalBottomSheet<void>(
                      context: context,
                      backgroundColor: _panel,
                      builder: (_) => ResourceDetail(resource: resource),
                    ),
                    title: Text(resource['title']?.toString() ?? 'Untitled'),
                    subtitle: Padding(
                      padding: const EdgeInsets.only(top: 8),
                      child: Text(
                        '${resource['type'] ?? 'Resource'} · ${resource['subject'] ?? 'General study'}',
                        style: const TextStyle(color: _gold, fontSize: 12),
                      ),
                    ),
                    trailing: IconButton(
                      onPressed: () => _toggle(resource),
                      color: _gold,
                      icon: Icon(saved.contains(id) ? Icons.bookmark : Icons.bookmark_border),
                    ),
                  ),
                );
              },
            ),
          ),
      ],
    );
  }
}

class ResourceDetail extends StatelessWidget {
  const ResourceDetail({required this.resource, super.key});

  final Map<String, dynamic> resource;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(24, 28, 24, 36),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(resource['type']?.toString().toUpperCase() ?? 'RESOURCE', style: const TextStyle(color: _gold, letterSpacing: 2, fontSize: 11)),
          const SizedBox(height: 12),
          Text(resource['title']?.toString() ?? 'Untitled', style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 14),
          Text(resource['description']?.toString() ?? 'Approved ARMAN learning material.', style: const TextStyle(color: _muted, height: 1.6)),
        ],
      ),
    );
  }
}

class SettingsPage extends StatefulWidget {
  const SettingsPage({required this.api, required this.session, super.key});

  final ArmanApi api;
  final Session session;

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  Map<String, dynamic> settings = {
    'visibility': 'public',
    'showStatistics': true,
    'showAchievements': true,
    'allowMessages': true,
  };
  bool loading = true;
  bool saving = false;
  String? error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final profile = await widget.api.profile();
      setState(() {
        settings = {
          'visibility': profile['visibility'] ?? 'public',
          'showStatistics': profile['showStatistics'] ?? true,
          'showAchievements': profile['showAchievements'] ?? true,
          'allowMessages': profile['allowMessages'] ?? true,
        };
        loading = false;
      });
    } catch (exception) {
      setState(() {
        error = exception.toString();
        loading = false;
      });
    }
  }

  Future<void> _save() async {
    setState(() => saving = true);
    try {
      await widget.api.updatePrivacy(settings);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Privacy settings saved.')));
      }
    } catch (exception) {
      setState(() => error = exception.toString());
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (loading) return const LoadingState();
    if (error != null) return ErrorState(message: error!);
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 56, 20, 32),
      children: [
        Text('Settings', style: Theme.of(context).textTheme.headlineMedium?.copyWith(fontWeight: FontWeight.w700)),
        const SizedBox(height: 6),
        const Text('Keep your space yours.', style: TextStyle(color: _muted)),
        const SizedBox(height: 28),
        DropdownButtonFormField<String>(
          value: settings['visibility'] as String,
          decoration: const InputDecoration(labelText: 'Profile visibility'),
          items: const [
            DropdownMenuItem(value: 'public', child: Text('Public')),
            DropdownMenuItem(value: 'private', child: Text('Private')),
          ],
          onChanged: (value) => setState(() => settings['visibility'] = value),
        ),
        const SizedBox(height: 12),
        for (final item in [
          ('showStatistics', 'Show statistics', 'Let your profile display learning statistics.'),
          ('showAchievements', 'Show achievements', 'Let others see achievements you share.'),
          ('allowMessages', 'Allow messages', 'Allow other students to send messages.'),
        ])
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            value: settings[item.$1] as bool,
            onChanged: (value) => setState(() => settings[item.$1] = value),
            title: Text(item.$2),
            subtitle: Text(item.$3, style: const TextStyle(color: _muted, fontSize: 12)),
            activeColor: _gold,
          ),
        const SizedBox(height: 20),
        FilledButton(
          onPressed: saving ? null : _save,
          style: FilledButton.styleFrom(backgroundColor: _gold, foregroundColor: _navy, minimumSize: const Size.fromHeight(52)),
          child: Text(saving ? 'Saving…' : 'Save privacy settings'),
        ),
        const SizedBox(height: 12),
        OutlinedButton(
          onPressed: () async {
            final navigator = Navigator.of(context);
            await widget.session.clear();
            if (!mounted) return;
            navigator.pushAndRemoveUntil(
              MaterialPageRoute<void>(builder: (_) => const BootstrapScreen()),
              (_) => false,
            );
          },
          child: const Text('Sign out'),
        ),
      ],
    );
  }
}

class LoadingState extends StatelessWidget {
  const LoadingState({super.key});

  @override
  Widget build(BuildContext context) => const Center(child: CircularProgressIndicator(color: _gold));
}

class EmptyState extends StatelessWidget {
  const EmptyState({super.key});

  @override
  Widget build(BuildContext context) => const Center(
        child: Padding(
          padding: EdgeInsets.all(28),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.auto_awesome, color: _gold, size: 32),
              SizedBox(height: 12),
              Text('Nothing here yet', style: TextStyle(fontWeight: FontWeight.w700)),
              SizedBox(height: 8),
              Text('Approved ARMAN learning resources will appear here.', textAlign: TextAlign.center, style: TextStyle(color: _muted)),
            ],
          ),
        ),
      );
}

class ErrorState extends StatelessWidget {
  const ErrorState({required this.message, super.key});

  final String message;

  @override
  Widget build(BuildContext context) => Center(
        child: Padding(
          padding: const EdgeInsets.all(28),
          child: Text(message, textAlign: TextAlign.center, style: const TextStyle(color: _muted)),
        ),
      );
}