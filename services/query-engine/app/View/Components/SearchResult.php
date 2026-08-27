<?php

namespace App\View\Components;

use Closure;
use Illuminate\Contracts\View\View;
use Illuminate\View\Component;

class SearchResult extends Component
{
    public $url;

    public $title;

    public $text;

    /**
     * Create a new component instance.
     */
    public function __construct($url, $title, $text)
    {
        $this->url = $url;
        $this->title = $title;
        $this->text = $text;
    }

    /**
     * Get the view / contents that represent the component.
     */
    public function render(): View|Closure|string
    {
        return view('components.search-result');
    }
}
