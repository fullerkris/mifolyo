// The server-rendered lists are the complete, keyboard-accessible navigation.
// Load the bundled graph library only when a reader opens the visual overview.
export function initConnectionMap(root) {
    const canvas = root.querySelector('[data-map-canvas]');
    const status = root.querySelector('[data-map-status]');
    const retry = root.querySelector('[data-map-retry]');
    const toolbar = root.querySelector('[data-map-toolbar]');
    const selection = root.querySelector('[data-map-selection]');
    let graph;
    let loading = false;
    const dense = root.querySelectorAll('[data-map-links] li').length > 12;
    const palette = () => {
        const css = getComputedStyle(root);
        const color = name => css.getPropertyValue(`--mf-${name}`).trim();
        return [
            { selector: 'node', style: { 'background-color': color('blue'), label: dense ? '' : 'data(label)', color: color('text'), 'font-size': 11, 'text-valign': 'bottom', 'text-margin-y': 9, 'text-wrap': 'ellipsis', 'text-max-width': 105, width: 26, height: 26, 'border-width': 2, 'border-color': color('surface') } },
            { selector: 'node[kind="current"]', style: { 'background-color': color('orange'), label: 'data(label)', width: 42, height: 42 } },
            { selector: 'node[kind="incoming"]', style: { 'background-color': color('green') } },
            { selector: 'node[kind="both"]', style: { 'background-color': color('purple') } },
            { selector: 'edge', style: { width: 1.5, 'line-color': color('muted'), 'target-arrow-color': color('muted'), 'target-arrow-shape': 'triangle', 'curve-style': 'bezier', opacity: .65 } },
            { selector: 'node:selected', style: { label: 'data(label)', 'border-color': color('text'), 'border-width': 4 } },
        ];
    };
    const showSelected = data => {
        selection.querySelector('[data-map-selected-title]').textContent = data.title;
        selection.querySelector('[data-map-selected-link]').href = data.url;
        selection.hidden = false;
    };
    const open = async () => {
        if (!root.open || loading) return;
        if (graph) { graph.resize(); graph.fit(undefined, 40); return; }
        loading = true;
        retry.hidden = true;
        status.textContent = 'Loading connection map…';
        try {
            const { default: cytoscape } = await import('cytoscape');
            // Opening can finish after the disclosure is closed; initialize on the next opening.
            if (!root.open) return;
            const source = root.dataset.sourceUrl;
            const title = root.dataset.sourceTitle;
            const label = text => [...text].slice(0, 28).join('');
            const elements = [{ data: { id: 'source', url: source, title, label: label(title), kind: 'current' } }];
            root.querySelectorAll('[data-map-links] li').forEach((link, index) => {
                const { url, outgoing, incoming } = link.dataset;
                if (url === source) return;
                const id = `page-${index}`;
                const title = link.dataset.title || new URL(url).hostname;
                elements.push({ data: { id, url, title, label: label(title), kind: outgoing === '1' && incoming === '1' ? 'both' : incoming === '1' ? 'incoming' : 'outgoing' } });
                if (outgoing === '1') elements.push({ data: { id: `out-${index}`, source: 'source', target: id } });
                if (incoming === '1') elements.push({ data: { id: `in-${index}`, source: id, target: 'source' } });
            });
            canvas.hidden = false;
            graph = cytoscape({
                container: canvas, elements, style: palette(), minZoom: .2, maxZoom: 3,
                userZoomingEnabled: false, autoungrabify: true, boxSelectionEnabled: false,
                layout: { name: 'concentric', animate: false, padding: 40, minNodeSpacing: dense ? 12 : 25, nodeDimensionsIncludeLabels: !dense, concentric: node => node.id() === 'source' ? 2 : 1, levelWidth: () => 1 },
            });
            graph.on('tap', 'node', event => showSelected(event.target.data()));
            showSelected(graph.getElementById('source').data());
            toolbar.hidden = false;
            status.textContent = 'Select a node to see its page. Arrows point from the linking page to its destination.';
            new ResizeObserver(() => { if (root.open) graph.resize(); }).observe(canvas);
            new MutationObserver(() => graph.style(palette())).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
        } catch (_) {
            graph?.destroy();
            graph = null;
            canvas.hidden = toolbar.hidden = selection.hidden = true;
            retry.hidden = false;
            status.textContent = 'The map could not load. Reload this page to try again, or explore the links in the lists below.';
        } finally { loading = false; }
    };
    root.addEventListener('toggle', open);
    // Browsers cache failed module imports; a reload allows a genuinely fresh request.
    retry.addEventListener('click', () => location.reload());
    root.querySelector('[data-map-fit]').addEventListener('click', () => graph?.fit(undefined, 40));
    root.querySelectorAll('[data-map-zoom]').forEach(button => button.addEventListener('click', () => {
        if (graph) graph.zoom({ level: graph.zoom() * (button.dataset.mapZoom === 'in' ? 1.3 : 1 / 1.3), renderedPosition: { x: graph.width() / 2, y: graph.height() / 2 } });
    }));
}
