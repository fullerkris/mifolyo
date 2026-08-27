FROM node:22.18.0-bookworm-slim

RUN apt-get update \
    && apt-get install --yes --no-install-recommends chromium ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --omit=dev \
    && npm cache clean --force

COPY worker.mjs worker.test.mjs smoke.mjs ./

RUN npm test \
    && mkdir -p /run/mifolyo-render \
    && chown 65534:65534 /run/mifolyo-render

ENV CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium \
    HOME=/tmp \
    NODE_ENV=production \
    RENDER_SOCKET=/run/mifolyo-render/renderer.sock

USER 65534:65534

CMD ["node", "worker.mjs"]
