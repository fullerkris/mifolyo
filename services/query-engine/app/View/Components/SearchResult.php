<?php

namespace App\View\Components;

use App\Support\SearchResultPresenter;
use Closure;
use Illuminate\Contracts\View\View;
use Illuminate\View\Component;

class SearchResult extends Component
{
    public $url;

    public $title;

    public $summary;

    public $description;

    public SearchResultPresenter $presenter;

    /**
     * Create a new component instance.
     */
    public function __construct($url, $title, $summary, $description, SearchResultPresenter $presenter)
    {
        $this->url = $url;
        $this->title = $title;
        $this->summary = $summary;
        $this->description = $description;
        $this->presenter = $presenter;
    }

    /**
     * Get the view / contents that represent the component.
     */
    public function render(): View|Closure|string
    {
        return view('components.search-result');
    }
}
