FROM node:22-bookworm-slim AS assets
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts
COPY . .
RUN npm run build

FROM php:8.4-fpm-bookworm
ARG WITH_MONGODB=false
RUN apt-get update \
    && apt-get install -y --no-install-recommends unzip libpq-dev libonig-dev libxml2-dev libssl-dev pkg-config \
    && docker-php-ext-install pdo_pgsql mbstring xml opcache \
    && if [ "$WITH_MONGODB" = true ]; then pecl install mongodb-1.21.0 && docker-php-ext-enable mongodb; fi \
    && rm -rf /var/lib/apt/lists/*
COPY --from=composer:2.8 /usr/bin/composer /usr/bin/composer
RUN cp "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini" \
    && printf 'opcache.enable=1\nopcache.validate_timestamps=0\nexpose_php=Off\n' > "$PHP_INI_DIR/conf.d/production.ini" \
    && printf '[www]\npm = dynamic\npm.max_children = 8\npm.start_servers = 2\npm.min_spare_servers = 1\npm.max_spare_servers = 3\npm.max_requests = 500\n' > /usr/local/etc/php-fpm.d/zz-production.conf \
    && useradd --uid 1000 --create-home --gid www-data app
WORKDIR /var/www
COPY . .
COPY --from=assets /app/public/build ./public/build
RUN COMPOSER_ALLOW_SUPERUSER=1 composer install --no-dev --no-interaction --prefer-dist --optimize-autoloader \
    && mkdir -p storage/framework/views storage/framework/cache/data storage/framework/sessions storage/logs bootstrap/cache /assets \
    && chown -R app:www-data storage bootstrap/cache /assets
USER app
EXPOSE 9000
CMD ["sh", "-ec", "php artisan config:cache && php artisan view:cache && exec php-fpm"]
