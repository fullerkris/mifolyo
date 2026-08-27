<?php

namespace App\View\Components;

use Closure;
use Illuminate\Contracts\View\View;
use Illuminate\View\Component;

class PaginationBar extends Component
{
    public $totalResults;

    public $totalPages;

    /**
     * Create a new component instance.
     */
    public function __construct($totalResults)
    {
        $this->totalResults = $totalResults;
        $this->totalPages = ceil($totalResults / 20);
    }

    /**
     * Get the view / contents that represent the component.
     */
    public function render(): View|Closure|string
    {
        return view('components.pagination-bar');
    }
}
