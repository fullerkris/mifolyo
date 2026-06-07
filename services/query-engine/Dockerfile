FROM php:8.3-fpm

ARG user=query-engine-user
ARG uid=1000

RUN apt-get update && apt-get install -y \
    git \
    curl \
    zip \
    unzip \
    libssl-dev \
    libcurl4-openssl-dev \
    pkg-config \
    libonig-dev \
    libxml2-dev \
    libpng-dev \
    libpq-dev \
    nodejs \
    npm

# Clear cache
RUN apt-get clean && rm -rf /var/lib/apt/lists/*

# Install php extensions
RUN docker-php-ext-install mbstring xml pdo_pgsql

# Install MongoDB extension
# RUN pecl install mongodb && docker-php-ext-enable mongodb
# Install MongoDB extension (version 1.x)
RUN pecl install mongodb-1.21.0 && docker-php-ext-enable mongodb

# install composer
COPY --from=composer:latest /usr/bin/composer /usr/bin/composer

# Create user
RUN useradd -u $uid -ms /bin/bash -g www-data $user

# Copy application
COPY . /var/www

COPY --chown=$user:www-data . /var/www

COPY docker-entrypoint.sh /usr/local/bin/

RUN chmod +x /usr/local/bin/docker-entrypoint.sh

WORKDIR /var/www

# Ensure Laravel cache data directory exists
RUN mkdir -p storage/framework/cache/data && \
    chown -R $user:www-data storage/framework/cache

RUN chown -R $user:www-data storage bootstrap/cache && \
    chmod -R ug+rwX storage bootstrap/cache

# Install frontend dependencies and build assets
RUN npm install && npm run build

# Install PHP dependencies
RUN composer install --no-dev --optimize-autoloader

USER $user

EXPOSE 9000

ENTRYPOINT ["docker-entrypoint.sh"]

CMD ["php-fpm"]
