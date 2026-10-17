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
    // Dedicated presentation examples do not change the 25-result baseline searches.
    $presentation = [
        ['_id' => 'https://example.test/ux05/caf%C3%A9?topic=c%2B%2B&lang=fr#examples', 'title' => 'CAFÉ & C++ — literal <em>indexed text</em>',
            'summary_text' => str_repeat('Synthetic opening material without a query match. ', 20).'A useful café guide explains C++ with literal <em>examples</em>. '.str_repeat('More synthetic context follows. ', 15), 'description' => 'A generic description.'],
        ['_id' => 'https://example.test/ux05/'.str_repeat('long-path/', 20).'?b=two&a=1#section', 'title' => str_repeat('A long café metadata title with useful detail ', 12), 'summary_text' => str_repeat('unbroken', 100), 'description' => ''],
        ['_id' => 'https://example.test/ux05/fallback', 'title' => null, 'summary_text' => " \n\t", 'description' => 'A description about café and C++.'],
        ['_id' => 'https://example.test/ux05/missing', 'title' => ['legacy' => 'malformed'], 'summary_text' => [], 'description' => null],
    ];
    foreach ($presentation as $index => $sample) {
        $url = $sample['_id'];
        unset($sample['_id']);
        $database->metadata->updateOne(['_id' => $url], ['$set' => $sample], ['upsert' => true]);
        $database->words->updateOne(['word' => 'ux05', 'url' => $url], ['$set' => ['weight' => 1.0 - $index / 100]], ['upsert' => true]);
    }
    // Image associations share artwork while preserving their individual source pages.
    $artwork = ['http://127.0.0.1:18081/staging-media/connections.svg', 'http://127.0.0.1:18081/staging-media/reading.svg', 'http://127.0.0.1:18081/mifolyo-mark.svg'];
    foreach ($pages as $index => [$pageUrl, $title]) {
        $imageUrl = $index === 23 ? 'http://127.0.0.1:18081/staging-media/missing.svg' : $artwork[$index % count($artwork)];
        $association = hash('sha256', $pageUrl."\0".$imageUrl);
        $database->images->updateOne(['_id' => $association], ['$set' => ['source_url' => $imageUrl, 'page_url' => $pageUrl, 'alt' => 'Synthetic staging artwork for '.$title, 'filename' => basename($imageUrl)]], ['upsert' => true]);
        foreach (['staging', 'ux06', 'gallery'] as $word) {
            $database->word_images->updateOne(['word' => $word, 'association_id' => $association], ['$set' => ['page_url' => $pageUrl, 'weight' => 1.0 - $index / 100]], ['upsert' => true]);
        }
    }
    $outgoing = array_merge(array_column(array_slice($pages, 1), 0), ['https://example.test/ux06/unindexed-outgoing']);
    $incoming = array_merge(array_column(array_slice($pages, 1, 22), 0), ['https://example.test/ux06/incoming-a', 'https://example.test/ux06/incoming-b', 'https://example.test/ux06/incoming-c']);
    $database->outlinks->updateOne(['_id' => $source], ['$set' => ['links' => $outgoing]], ['upsert' => true]);
    $database->backlinks->updateOne(['_id' => $source], ['$set' => ['links' => $incoming]], ['upsert' => true]);
    foreach ($outgoing as $target) {
        $database->backlinks->updateOne(['_id' => $target], ['$addToSet' => ['links' => $source]], ['upsert' => true]);
    }
    foreach ($incoming as $origin) {
        $database->outlinks->updateOne(['_id' => $origin], ['$addToSet' => ['links' => $source]], ['upsert' => true]);
    }
    echo 'Seeded '.count($pages).' baseline sources, '.count($presentation)." UX-05 examples, 25 image associations and bidirectional UX-06 connections.\n";
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
