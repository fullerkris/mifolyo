<?php

namespace App\View\Components;

use Closure;
use Illuminate\Contracts\View\View;
use Illuminate\View\Component;

class PaginationBar extends Component
{
    public $totalResults;

    public $totalPages;

    public $currentPage;

    public $pages;

    /**
     * Create a new component instance.
     */
    public function __construct($totalResults, $perPage = 20)
    {
        $this->totalResults = $totalResults;
        $this->totalPages = min(100, (int) ceil($totalResults / $perPage));
        $page = filter_var(request()->query('page', 1), FILTER_VALIDATE_INT);
        $this->currentPage = max(1, min(100, $page ?: 1));
        $start = max(1, min($this->currentPage - 2, $this->totalPages - 4));
        $this->pages = $this->totalPages > 0 ? range($start, min($this->totalPages, $start + 4)) : [];
    }

    /**
     * Get the view / contents that represent the component.
     */
    public function render(): View|Closure|string
    {
        return view('components.pagination-bar');
    }
}
