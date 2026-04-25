<?php

namespace Tests\Feature;

use Database\Seeders\BaselineDataSeeder;
use Illuminate\Foundation\Testing\RefreshDatabase;
use Tests\TestCase;

class BaselineSeederTest extends TestCase
{
    use RefreshDatabase;

    public function test_baseline_seeder_creates_expected_seed_data(): void
    {
        $this->seed(BaselineDataSeeder::class);

        $this->assertDatabaseHas('users', [
            'email' => 'admin@mifolyo.local',
        ]);

        $this->assertDatabaseHas('communities', [
            'slug' => 'general',
        ]);

        $this->assertDatabaseHas('communities', [
            'slug' => 'announcements',
        ]);

        $this->assertDatabaseHas('posts', [
            'slug' => 'welcome-to-mifolyo',
        ]);

        $this->assertDatabaseHas('posts', [
            'slug' => 'forum-engine-alpha',
        ]);

        $this->assertDatabaseCount('community_memberships', 4);
    }
}
