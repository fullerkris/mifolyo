<?php

// Run only through the dedicated local staging Compose project.
require '/var/www/vendor/autoload.php';
$app = require '/var/www/bootstrap/app.php';
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();

if (! $app->environment('staging') || getenv('MIFOLYO_LOCAL_SEARCH_STAGING') !== '1') {
    throw new RuntimeException('This seed requires the local Search UI staging environment.');
}

$source = 'https://developer.mozilla.org/en-US/docs/Web/API/URLSearchParams?topic=search&lang=en';
$mode = $argv[1] ?? '';

if ($mode === 'query') {
    if (config('database.connections.mongodb.database') !== 'mifolyo_search_ui_staging'
        || config('database.connections.mongodb.dsn') !== 'mongodb://mongo:27017/?serverSelectionTimeoutMS=2000&serverSelectionTryOnce=false') {
        throw new RuntimeException('Unexpected staging MongoDB target.');
    }
    $database = (new MongoDB\Client(config('database.connections.mongodb.dsn')))
        ->selectDatabase('mifolyo_search_ui_staging');
    $pages = [[$source, 'URLSearchParams — search query parameters']];
    foreach (['requests', 'responses', 'routing', 'validation', 'pagination', 'collections', 'queries', 'eloquent', 'migrations', 'seeding', 'cache', 'redis', 'http-client', 'testing', 'http-tests', 'browser-tests', 'blade', 'vite', 'configuration', 'errors', 'logging', 'authentication', 'authorization', 'deployment'] as $topic) {
        $pages[] = ['https://laravel.com/docs/12.x/'.$topic, 'Laravel: '.ucfirst(str_replace('-', ' ', $topic))];
    }
    foreach ($pages as $index => [$url, $title]) {
        $database->metadata->updateOne(['_id' => $url], ['$set' => [
            'title' => $title,
            'description' => 'Local staging reference page.',
            'summary_text' => 'Local staging sample: explore this documentation source and its URL-linked discussions. Metadata was seeded for integration validation, not crawled from the website.',
            'last_crawled' => '',
        ]], ['upsert' => true]);
        foreach (['staging', 'search', 'documentation'] as $word) {
            $database->words->updateOne(['word' => $word, 'url' => $url], ['$set' => ['weight' => 1.0 - $index / 100]], ['upsert' => true]);
            $database->dictionary->updateOne(['_id' => $word], ['$setOnInsert' => ['word' => $word]], ['upsert' => true]);
        }
    }
    echo 'Seeded '.count($pages)." indexed sources. Search for staging, search, or documentation.\n";
} elseif ($mode === 'forum') {
    if (config('database.connections.pgsql.database') !== 'mifolyo_search_ui_staging'
        || config('database.connections.pgsql.host') !== 'postgres') {
        throw new RuntimeException('Unexpected staging PostgreSQL target.');
    }
    Illuminate\Support\Facades\DB::transaction(function () use ($source): void {
        $user = App\Models\User::query()->firstOrCreate(['email' => 'reader@staging.mifolyo.test'], [
            'name' => 'Staging Reader', 'username' => 'staging_reader', 'password' => 'local-staging-only-password',
        ]);
        $community = App\Models\Community::query()->firstOrCreate(['slug' => 'general'], [
            'owner_user_id' => $user->id, 'name' => 'Local staging',
            'description' => 'Controlled discussions for Search UI integration validation.', 'is_private' => false,
        ]);
        App\Models\CommunityMembership::query()->firstOrCreate(['community_id' => $community->id, 'user_id' => $user->id], ['role' => 'owner']);
        $normalized = App\Support\SourceUrlNormalizer::normalizeV1($source);
        for ($number = 1; $number <= 23; $number++) {
            $post = App\Models\Post::query()->firstOrCreate(['community_id' => $community->id, 'slug' => 'staging-source-discussion-'.$number], array_merge($normalized, [
                'author_user_id' => $user->id, 'title' => sprintf('Source discussion %02d', $number),
                'body' => 'This is a seeded local staging conversation. The query service retrieves it from the real forum API and PostgreSQL. Use the sort and pagination controls to browse the conversations attached to this exact URL.',
                'url' => $normalized['source_url'], 'content_type' => 'link',
                'published_at' => now()->subDays(2)->addMinutes($number),
            ]));
            App\Models\Comment::query()->firstOrCreate([
                'post_id' => $post->id, 'author_user_id' => $user->id, 'body' => 'A persisted staging reply connected to this discussion.',
            ], ['depth' => 0]);
            if ($number === 1) {
                App\Models\Vote::query()->firstOrCreate([
                    'user_id' => $user->id, 'votable_type' => App\Models\Post::class, 'votable_id' => $post->id,
                ], ['value' => 1]);
            }
            $upvotes = $post->votes()->where('value', 1)->count();
            $downvotes = $post->votes()->where('value', -1)->count();
            $post->forceFill([
                'comment_count' => $post->comments()->count(), 'upvote_count' => $upvotes,
                'downvote_count' => $downvotes, 'score' => $upvotes - $downvotes,
            ])->save();
        }
        $community->forceFill(['member_count' => $community->memberships()->count(), 'post_count' => $community->posts()->count()])->save();
    });
    echo "Seeded 23 source-linked discussions and replies in the staging forum.\n";
} else {
    throw new InvalidArgumentException('Choose query or forum.');
}
