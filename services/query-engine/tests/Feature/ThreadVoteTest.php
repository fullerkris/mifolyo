<?php

namespace Tests\Feature;

use App\Services\ForumApi;
use Illuminate\Support\Facades\Crypt;
use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Route;
use Tests\TestCase;

class ThreadVoteTest extends TestCase
{
    private array $input = ['votable_type' => 'comment', 'votable_id' => 4, 'value' => 1];

    private string $from = '/images?q=C%2B%2B%20%26%20caf%C3%A9&page=2';

    protected function setUp(): void
    {
        parent::setUp();
        config(['services.forum.base_url' => 'http://forum.test']);
        Http::preventStrayRequests();
    }

    private function forum(array $overrides = []): void
    {
        Http::fake($overrides + [
            'forum.test/api/auth/me' => Http::response(['data' => ['id' => 3, 'username' => 'reader']]),
            'forum.test/api/posts/12' => Http::response(['data' => ['id' => 12, 'title' => 'Thread', 'score' => 7, 'viewer_vote' => 1]]),
            'forum.test/api/posts/12/comments' => Http::response(['data' => [
                ['id' => 4, 'body' => 'Comment', 'score' => -2, 'viewer_vote' => -1, 'children' => []],
                ['id' => 5, 'body' => 'Closed comment', 'score' => 0, 'viewer_vote' => 0, 'is_locked' => true, 'children' => []],
                ['id' => 6, 'body' => '[Comment removed]', 'score' => 3, 'viewer_vote' => 0, 'is_removed' => true, 'children' => []],
            ]]),
        ]);
    }

    private function signIn(): void
    {
        $this->withSession(['forum' => ['token' => Crypt::encryptString('server-token'), 'user' => ['id' => 3, 'username' => 'reader']]]);
    }

    public function test_authenticated_read_renders_scores_selection_and_restricted_controls(): void
    {
        $this->forum();
        $this->signIn();
        $this->get('/threads/12')->assertOk()->assertSee('id="thread-votes"', false)
            ->assertSee('data-current-vote="1"', false)->assertSee('data-current-vote="-1"', false)
            ->assertSee('data-vote-score>7</span>', false)->assertSee('data-vote-score>-2</span>', false)
            ->assertSee('Voting closed')->assertSee('Voting unavailable for removed content.')
            ->assertDontSee('server-token');
    }

    public function test_guest_sign_in_returns_to_exact_comment_query_and_reply_target(): void
    {
        $back = '/threads/12?'.http_build_query(['from' => $this->from, 'reply_to' => 4]).'#comment-4';
        $this->post('/threads/12/votes', $this->input + ['from' => $this->from, 'reply_to' => 4])
            ->assertRedirect(route('account.sign-in', ['return_to' => $back]));
        $this->postJson('/threads/12/votes', $this->input)->assertUnauthorized();
        Http::assertNothingSent();
        $this->forum();
        $this->get('/threads/12?'.http_build_query(['from' => $this->from, 'reply_to' => 4]))
            ->assertOk()->assertSee(e(route('account.sign-in', ['return_to' => $back])), false)->assertSee('Sign in to vote');
    }

    public function test_votes_send_only_explicit_values_and_server_credentials_bound_to_route_thread(): void
    {
        $this->forum(['forum.test/api/votes' => Http::response(['data' => ['viewer_vote' => 0, 'votable' => ['type' => 'comment', 'id' => 4, 'score' => 2]]])]);
        $this->signIn();
        $response = $this->postJson('/threads/12/votes', ['votable_type' => 'comment', 'votable_id' => 4, 'value' => 0, 'user_id' => 999, 'post_id' => 99, 'score' => 500], ['Authorization' => 'Bearer browser-token']);
        $response->assertOk()->assertExactJson(['data' => ['type' => 'comment', 'id' => 4, 'score' => 2, 'viewer_vote' => 0], 'message' => 'Vote removed.']);
        $this->assertStringContainsString('no-store', $response->headers->get('Cache-Control'));
        Http::assertSent(fn ($request) => $request->url() === 'http://forum.test/api/votes' && $request->data() === ['votable_type' => 'comment', 'votable_id' => 4, 'value' => 0, 'post_id' => 12]
            && $request->hasHeader('Authorization', 'Bearer server-token'));
    }

    public function test_invalid_votes_and_csrf_fail_before_any_write(): void
    {
        foreach ([['value' => 3], ['value' => []], ['votable_type' => []], ['votable_type' => 'page'], ['votable_id' => -1], ['votable_type' => 'post', 'votable_id' => 99]] as $invalid) {
            $this->postJson('/threads/12/votes', array_replace($this->input, $invalid))->assertUnprocessable();
        }
        $this->app['env'] = 'local';
        $this->postJson('/threads/12/votes', $this->input)->assertStatus(419);
        Http::assertNothingSent();
    }

    public function test_failed_votes_keep_safe_statuses_and_retry_after_without_leaking_service_details(): void
    {
        $sequence = Http::sequence();
        foreach ([403, 404, 422, 423, 429, 500] as $status) {
            $sequence->push(['message' => 'private upstream internals'], $status, ['Retry-After' => '17']);
        }
        $this->forum(['forum.test/api/votes' => $sequence]);
        $this->signIn();
        foreach ([403, 404, 422, 423, 429, 503] as $status) {
            $response = $this->postJson('/threads/12/votes', $this->input)->assertStatus($status)->assertDontSee('private upstream internals');
            if ($status === 429) {
                $response->assertHeader('Retry-After', '17');
            }
        }
        Http::assertSentCount(12); // One account read + one write per attempt; no automatic retries.
    }

    public function test_uncertain_html_write_keeps_the_exact_retry_value_and_reading_destination(): void
    {
        $this->forum(['forum.test/api/votes' => Http::failedConnection()]);
        $this->signIn();
        $input = array_replace($this->input, ['value' => 0, 'from' => $this->from]);
        $back = '/threads/12?'.http_build_query(['from' => $this->from]).'#comment-4';
        $this->post('/threads/12/votes', $input)->assertRedirect($back)
            ->assertSessionHas('vote_feedback.retry_value', 0)->assertSessionHas('vote_feedback.id', 4);
        $this->get($back)->assertOk()->assertSee('Retry vote')->assertSee('Retry to save the same choice.');
    }

    public function test_malformed_success_is_unconfirmed_and_cannot_update_a_different_target(): void
    {
        $this->forum(['forum.test/api/votes' => Http::sequence()
            ->push(['data' => ['viewer_vote' => 1, 'votable' => ['type' => 'comment', 'id' => 99, 'score' => 2]]])
            ->push(['data' => ['viewer_vote' => 1, 'votable' => ['type' => 'comment', 'id' => 4, 'score' => '2']]])
            ->push(['data' => ['viewer_vote' => -1, 'votable' => ['type' => 'comment', 'id' => 4, 'score' => 2]]])]);
        $this->signIn();
        for ($attempt = 0; $attempt < 3; $attempt++) {
            $this->postJson('/threads/12/votes', $this->input)->assertStatus(503)->assertJsonMissingPath('data');
        }
    }

    public function test_expired_session_returns_to_comment_after_sign_in_without_replaying_the_vote(): void
    {
        $this->forum(['forum.test/api/votes' => Http::response(['message' => 'Token expired'], 401)]);
        $this->signIn();
        $this->post('/threads/12/votes', $this->input)->assertRedirect(route('account.sign-in', ['return_to' => '/threads/12#comment-4']))->assertSessionMissing('forum');
        Http::assertSentCount(2);
    }

    public function test_middleware_rate_limit_returns_to_a_readable_thread_route_with_context(): void
    {
        Route::post('/threads/{thread}/votes', fn () => abort(429, '', ['Retry-After' => '23']))->name('threads.vote');
        $back = '/threads/12?'.http_build_query(['from' => $this->from]).'#comment-4';
        $this->post('/threads/12/votes', $this->input + ['from' => $this->from])->assertStatus(429)->assertHeader('Retry-After', '23')
            ->assertSee('href="'.e($back).'"', false)->assertSee('Return to conversation');
        Http::assertNothingSent();
    }

    public function test_only_known_thread_anchors_are_allowed_for_sign_in_returns(): void
    {
        foreach (['/threads/12#comment-4', '/threads/12#thread-votes', '/threads/12?'.http_build_query(['from' => $this->from]).'#comment-4'] as $url) {
            $this->assertSame($url, ForumApi::returnTo($url));
            $this->get('/sign-in?'.http_build_query(['return_to' => $url]))->assertOk()->assertSee('value="'.e($url).'"', false);
        }
        foreach (['//evil.test/threads/12#comment-4', '/threads/12#untrusted', '/threads/12#comment-0', '/threads/12#mf-return', '/search?q=a#comment-4', '/threads/12?x=\\bad#comment-4'] as $url) {
            $this->assertSame('/', ForumApi::returnTo($url));
        }
    }
}
