<?php

// Feed to PHP via exec -T in the isolated production-topology smoke project.
// Never load this fixture as application code or against real production data.
require '/var/www/vendor/autoload.php';
$app = require '/var/www/bootstrap/app.php';
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();
set_exception_handler(function (Throwable $exception): void {
    file_put_contents('php://stderr', $exception->getMessage()."\n");
    exit(1);
});
if (getenv('MIFOLYO_PRODUCTION_SMOKE') !== '1'
    || config('app.url') !== 'https://environment-smoke.example.ts.net') {
    throw new RuntimeException('This fixture requires the isolated production smoke configuration.');
}

function expectStatus(Illuminate\Http\Client\Response $response, int $status): array
{
    if ($response->status() !== $status) {
        throw new RuntimeException('Unexpected API status: '.$response->status());
    }

    return $response->json();
}

$client = Illuminate\Support\Facades\Http::baseUrl('http://caddy:8080/api')->acceptJson()->timeout(10);
$suffix = bin2hex(random_bytes(6));
$email = 'environment-'.$suffix.'@smoke.mifolyo.test';
$account = expectStatus($client->post('/auth/register', [
    'name' => 'Environment Smoke', 'username' => 'env_'.$suffix, 'email' => $email,
    'password' => 'local-smoke-password', 'password_confirmation' => 'local-smoke-password',
]), 201);
$client = $client->withToken($account['data']['token']);
$community = expectStatus($client->post('/communities', ['name' => 'Environment '.$suffix, 'description' => 'Local smoke fixture.']), 201);
$post = expectStatus($client->post('/posts', [
    'community_slug' => $community['data']['slug'], 'title' => 'Environment smoke post',
    'content_type' => 'text', 'body' => 'Local smoke fixture.',
]), 201);
$id = $post['data']['id'];
expectStatus($client->post('/posts/'.$id.'/comments', ['body' => 'Local smoke reply.']), 201);
expectStatus($client->post('/votes', ['votable_type' => 'post', 'votable_id' => $id, 'value' => 1]), 200);
expectStatus($client->post('/reports', ['reportable_type' => 'post', 'reportable_id' => $id, 'reason' => 'Local smoke report.']), 201);
expectStatus($client->post('/auth/forgot-password', ['email' => $email]), 202);

// Use the actual configured session handler, not a raw Redis key.
$session = app('session')->driver();
$session->setId(str_repeat('c', 40));
$session->start();
$session->put('environment-smoke', 'persistent-session');
$session->save();

$index = (new MongoDB\Client(config('database.connections.mongodb.dsn')))->selectDatabase('mifolyo_index');
try {
    $index->metadata->insertOne(['_id' => 'forbidden-smoke-write']);
    throw new RuntimeException('Query Engine unexpectedly has MongoDB write permission.');
} catch (MongoDB\Driver\Exception\BulkWriteException $exception) {
    if ($exception->getCode() !== 13) {
        throw $exception;
    }
}

echo "PASS: production FPM account/community/post/reply/vote/report/reset-email API flow; Redis session saved; MongoDB writes denied.\n";
