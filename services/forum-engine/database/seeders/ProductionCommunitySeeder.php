<?php

namespace Database\Seeders;

use App\Models\Community;
use Illuminate\Database\Seeder;

class ProductionCommunitySeeder extends Seeder
{
    public function run(): void
    {
        Community::query()->firstOrCreate(
            ['slug' => 'general'],
            [
                'name' => 'General',
                'description' => 'Discuss and share useful sources with the MiFolyo community.',
                'is_private' => false,
                'is_nsfw' => false,
            ],
        );
    }
}
