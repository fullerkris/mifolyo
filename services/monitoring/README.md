# Monitoring

This service is responsible for monitoring the other services and spawning new instances as needed. It is not fully implemented yet, but it is a placeholder for future development.

The crawl backlog is read with `ZCARD` from `CRAWL_QUEUE_KEY`. The variable is
optional and defaults to the V1 queue, `mifolyo:crawl:v1:queue`, so monitoring
and the spider observe the same versioned backlog. Set it explicitly only when
all V1 queue producers and consumers are intentionally configured to use the
same alternate key.
