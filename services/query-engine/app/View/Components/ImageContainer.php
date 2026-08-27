<?php

namespace App\View\Components;

use Closure;
use Illuminate\Contracts\View\View;
use Illuminate\View\Component;

class ImageContainer extends Component
{
    public $url;

    public $alt;

    public $title;

    public $page_url;

    public $text;

    /**
     * Create a new component instance.
     */
    public function __construct($url, $alt, $title, $page, $text)
    {
        $this->url = $url;
        $this->alt = $alt;
        $this->title = $title;
        $this->page_url = $page;
        $this->text = $text;
    }

    /**
     * Get the view / contents that represent the component.
     */
    public function render(): View|Closure|string
    {
        return view('components.image-container');
    }
}
