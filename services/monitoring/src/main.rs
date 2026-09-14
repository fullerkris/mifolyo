extern crate redis;
use redis::Commands;
use std::env;
use std::process;
use std::process::Command;
use std::{thread, time};

const DEFAULT_CRAWL_QUEUE_KEY: &str = "mifolyo:crawl:v1:queue";
const BACKLINKS_SCAN_LIMIT: usize = 100;
const BACKLINKS_SCAN_RESPONSE_LIMIT: usize = 1024;

fn main() {
    // Get environment variable
    let redis_host = env::var("REDIS_HOST").expect("REDIS_HOST environment variable not set");
    let redis_port = env::var("REDIS_PORT").unwrap_or_else(|_| "6379".to_string());
    let redis_password = env::var("REDIS_PASSWORD").unwrap_or_else(|_| "".to_string());
    let redis_db: u8 = env::var("REDIS_DB")
        .unwrap_or("0".to_string())
        .parse()
        .expect("REDIS_DB must be a valid integer");
    let crawl_queue_key =
        env::var("CRAWL_QUEUE_KEY").unwrap_or_else(|_| DEFAULT_CRAWL_QUEUE_KEY.to_string());

    // Build the Redis URL
    let mut redis_url = format!("redis://{}:{}", redis_host, redis_port);

    // Add password if available
    if !redis_password.is_empty() {
        redis_url = format!("redis://:{}@{}:{}", redis_password, redis_host, redis_port);
    }

    // Add the DB number
    redis_url = format!("{}/{}", redis_url, redis_db);

    println!("Connecting to Redis...");
    // Connect to redis
    let client = redis::Client::open(redis_url).expect("Invalid Redis URL");
    let mut con = client.get_connection().expect("Failed to connect to Redis");

    // Get command line arguments for max services
    let args: Vec<String> = env::args().collect();

    let max_spiders = if args.len() > 1 {
        args[1].parse::<usize>().unwrap_or(10) // Changed default to 10
    } else {
        10 // Default to 10
    };

    let max_indexers = if args.len() > 2 {
        args[2].parse::<usize>().unwrap_or(10)
    } else {
        10 // Default to 10
    };

    let max_backlinks_processors = if args.len() > 3 {
        args[3].parse::<usize>().unwrap_or(5)
    } else {
        5 // Default to 5
    };

    let mut backlinks_scan = BacklinksScan::default();
    loop {
        // Check for termination signal
        if let Ok(Some(message)) = con.rpop::<_, Option<String>>("terminator_queue", None) {
            if message == "TERMINATE_SERVICES" {
                println!("Received termination signal. Shutting down services...");
                terminate_services();
                break;
            }
        }

        // Get the length of the configured V1 crawl queue.
        let url_queue_length: usize = con
            .zcard(crawl_queue_key.as_str())
            .expect("Failed to get queue length");
        // Get the length of the indexer_queue
        let indexer_queue_length: usize =
            con.llen("pages_queue").expect("Failed to get queue length");
        // Get the count of backlinks keys
        let backlinks_count = match get_backlinks_count(&mut backlinks_scan, |cursor| {
            redis::cmd("SCAN")
                .cursor_arg(cursor)
                .arg("MATCH")
                .arg("backlinks:*")
                .arg("COUNT")
                .arg(BACKLINKS_SCAN_LIMIT)
                .query(&mut con)
        }) {
            Ok(count) => count,
            Err(error) => {
                eprintln!("Backlinks scan failed; retaining scan progress: {}", error);
                None
            }
        };

        // Print the queue lengths and backlinks count
        println!("|--------------------------------------|");
        println!(
            "| Current crawl queue ({}) length: {}",
            crawl_queue_key, url_queue_length
        );
        println!("| Current indexer_queue length: {}", indexer_queue_length);
        if let Some(count) = backlinks_count {
            println!("| Current backlinks count: {}", count);
        } else {
            println!("| Backlinks count pending/unavailable; scaling unchanged");
        }
        println!("|--------------------------------------|");

        // Scale spiders
        let desired_spiders = {
            if url_queue_length == 0 || indexer_queue_length >= 1000 {
                0 // No spiders without crawl work or when the indexer is full.
            } else {
                let scale_factor = 1.0 - (indexer_queue_length as f64 / 1000.0);
                (max_spiders as f64 * scale_factor).round() as usize
            }
        };

        let current_spiders = get_current_spiders();
        println!("| Current spiders: {}", current_spiders);
        println!("|\tDesired spider count: {}", desired_spiders);
        scale_spiders(desired_spiders);

        // Scale indexers
        let desired_indexers = {
            if indexer_queue_length < 50 {
                1
            } else {
                std::cmp::min(
                    max_indexers,
                    (1.5 * (indexer_queue_length as f64 / 100.0)).ceil() as usize,
                )
            }
        };

        let desired_indexers = std::cmp::min(desired_indexers, max_indexers);
        let current_indexers = get_current_indexers();
        println!("| Current indexers: {}", current_indexers);
        println!("|\tDesired indexer count: {}", desired_indexers);
        scale_indexers(desired_indexers);

        // Scale backlinks processors
        if let Some(count) = backlinks_count {
            let desired_backlinks_processors =
                calculate_desired_backlinks_processors(count, max_backlinks_processors);
            let current_backlinks_processors = get_current_backlinks_processors();
            println!(
                "| Current backlinks processors: {}",
                current_backlinks_processors
            );
            println!(
                "|\tDesired backlinks processors count: {}",
                desired_backlinks_processors
            );
            scale_backlinks_processors(desired_backlinks_processors);
        }

        println!("|--------------------------------------|");
        println!();

        // Sleep
        for _i in 0..5 {
            thread::sleep(time::Duration::from_millis(1000));
            // Check for termination signal
            if let Ok(Some(message)) = con.rpop::<_, Option<String>>("terminator_queue", None) {
                if message == "TERMINATE_SERVICES" {
                    println!("Received termination signal. Shutting down services...");
                    terminate_services();
                }
            }
        }
    }
}

#[derive(Default)]
struct BacklinksScan {
    cursor: u64,
    count: usize,
    pending_keys: usize,
}

fn get_backlinks_count(
    state: &mut BacklinksScan,
    scan_page: impl FnOnce(u64) -> redis::RedisResult<(u64, Vec<Vec<u8>>)>,
) -> redis::RedisResult<Option<usize>> {
    if state.pending_keys == 0 {
        let (next_cursor, keys) = scan_page(state.cursor)?;
        if keys.len() > BACKLINKS_SCAN_RESPONSE_LIMIT {
            return Err((
                redis::ErrorKind::ClientError,
                "backlinks_scan_response_too_large",
            )
                .into());
        }
        state.pending_keys = keys.len();
        state.cursor = next_cursor;
    }

    let accounted = state.pending_keys.min(BACKLINKS_SCAN_LIMIT);
    state.count = state.count.saturating_add(accounted);
    state.pending_keys -= accounted;
    if state.pending_keys == 0 && state.cursor == 0 {
        let count = state.count;
        state.count = 0;
        Ok(Some(count))
    } else {
        Ok(None)
    }
}

fn calculate_desired_backlinks_processors(backlinks_count: usize, max_processors: usize) -> usize {
    if backlinks_count < 1000 {
        1
    } else {
        // Scale linearly from 1 to max_processors
        let scale_factor = (backlinks_count as f64 / 50000.0).min(1.0);
        let calculated = 1 + ((max_processors - 1) as f64 * scale_factor).round() as usize;
        std::cmp::min(calculated, max_processors)
    }
}

fn scale_backlinks_processors(desired_processors: usize) {
    let current_processors = get_current_backlinks_processors();

    if current_processors != desired_processors {
        println!("Scaling backlinks processors to: {}", desired_processors);
        Command::new("docker")
            .arg("compose")
            .arg("-f")
            .arg("../backlinks-processor/docker-compose.yml") // Specify the backlinks service compose file
            .arg("up")
            //.arg("--scale")
            //.arg(format!("backlinks-processor={}", desired_processors))
            .arg("-d")
            .status()
            .expect("Failed to scale backlinks processors");
    }
}

fn get_current_backlinks_processors() -> usize {
    let output = Command::new("docker")
        .arg("compose")
        .arg("-f")
        .arg("../backlinks-processor/docker-compose.yml") // Specify the backlinks service compose file
        .arg("ps")
        .output()
        .expect("Failed to get Docker Compose services");

    let output_str = String::from_utf8_lossy(&output.stdout);

    output_str
        .split('\n')
        .filter(|line| line.contains("backlinks-processor"))
        .count()
}

fn stop_backlinks_processors() {
    println!("Stopping backlinks processors...");
    Command::new("docker")
        .arg("compose")
        .arg("-f")
        .arg("../backlinks-processor/docker-compose.yml") // Specify the backlinks service compose file
        .arg("down")
        .status()
        .expect("Failed to stop backlinks processors");
}

fn terminate_services() {
    println!("Terminating services...");
    stop_indexers();
    stop_spiders();
    stop_backlinks_processors();
    process::exit(0);
}

fn scale_spiders(desired_spiders: usize) {
    let current_spiders = get_current_spiders();

    if current_spiders != desired_spiders {
        println!("Scaling spiders to: {}", desired_spiders);
        Command::new("docker")
            .arg("compose")
            .arg("-f")
            .arg("../spider/docker-compose.yml") // Specify the indexer service compose file
            .arg("up")
            .arg("--scale")
            .arg(format!("spider-service={}", desired_spiders))
            .arg("-d")
            .status()
            .expect("Failed to scale spiders");
    }
}

fn get_current_spiders() -> usize {
    let output = Command::new("docker")
        .arg("compose")
        .arg("-f")
        .arg("../spider/docker-compose.yml") // Specify the indexer service compose file
        .arg("ps")
        .output()
        .expect("Failed to get Docker Compose services");

    let output_str = String::from_utf8_lossy(&output.stdout);

    output_str
        .split('\n')
        .filter(|line| line.contains("spider-service"))
        .count()
}

fn scale_indexers(desired_indexers: usize) {
    let current_indexers = get_current_indexers();

    if current_indexers != desired_indexers {
        println!("Scaling indexers to: {}", desired_indexers);
        Command::new("docker")
            .arg("compose")
            .arg("-f")
            .arg("../indexer/docker-compose.yml") // Specify the indexer service compose file
            .arg("up")
            .arg("--scale")
            .arg(format!("indexer-service={}", desired_indexers))
            .arg("-d")
            .status()
            .expect("Failed to scale indexers");
    }
}

fn stop_indexers() {
    println!("Stopping indexers...");
    Command::new("docker")
        .arg("compose")
        .arg("-f")
        .arg("../indexer/docker-compose.yml") // Specify the indexer service compose file
        .arg("down")
        .status()
        .expect("Failed to stop indexers");
}

fn stop_spiders() {
    println!("Stopping spiders...");
    Command::new("docker")
        .arg("compose")
        .arg("-f")
        .arg("../spider/docker-compose.yml") // Specify the indexer service compose file
        .arg("down")
        .status()
        .expect("Failed to stop spiders");
}

fn get_current_indexers() -> usize {
    let output = Command::new("docker")
        .arg("compose")
        .arg("-f")
        .arg("../indexer/docker-compose.yml") // Specify the indexer service compose file
        .arg("ps")
        .output()
        .expect("Failed to get Docker Compose services");

    let output_str = String::from_utf8_lossy(&output.stdout);

    output_str
        .split('\n')
        .filter(|line| line.contains("indexer-service"))
        .count()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scan_advances_one_bounded_page_per_tick() {
        let mut state = BacklinksScan::default();
        let mut calls = 0;
        let count = get_backlinks_count(&mut state, |cursor| {
            calls += 1;
            assert_eq!(cursor, 0);
            Ok((7, vec![vec![]; 100]))
        })
        .unwrap();
        assert_eq!(calls, 1);
        assert_eq!(count, None);
        assert_eq!((state.cursor, state.count), (7, 100));

        let count = get_backlinks_count(&mut state, |cursor| {
            calls += 1;
            assert_eq!(cursor, 7);
            Ok((0, vec![vec![]; 100]))
        })
        .unwrap();
        assert_eq!(calls, 2);
        assert_eq!(count, Some(200));
        assert_eq!((state.cursor, state.count), (0, 0));
    }

    #[test]
    fn accepted_large_pages_drain_before_scanning_or_publishing() {
        for page_size in [101, 1024] {
            for next_cursor in [0, 7] {
                let mut state = BacklinksScan::default();
                assert_eq!(
                    get_backlinks_count(&mut state, |cursor| {
                        assert_eq!(cursor, 0);
                        Ok((next_cursor, vec![vec![]; page_size]))
                    })
                    .unwrap(),
                    None
                );
                assert_eq!(state.cursor, next_cursor);
                assert_eq!(state.count, 100);
                assert_eq!(state.pending_keys, page_size - 100);

                let mut accounted = 100;
                while accounted < page_size {
                    let count = get_backlinks_count(&mut state, |_| {
                        panic!("SCAN called while draining pending keys")
                    })
                    .unwrap();
                    accounted = (accounted + 100).min(page_size);
                    assert_eq!(state.cursor, next_cursor);
                    assert_eq!(state.pending_keys, page_size - accounted);
                    if accounted == page_size && next_cursor == 0 {
                        assert_eq!(count, Some(page_size));
                        assert_eq!(state.count, 0);
                    } else {
                        assert_eq!(count, None);
                        assert_eq!(state.count, accounted);
                    }
                }
                assert_eq!(
                    get_backlinks_count(&mut state, |cursor| {
                        assert_eq!(cursor, next_cursor);
                        Ok((0, vec![]))
                    })
                    .unwrap(),
                    Some(if next_cursor == 0 { 0 } else { page_size })
                );
            }
        }
    }

    #[test]
    fn empty_nonterminal_pages_do_not_publish_zero() {
        let mut state = BacklinksScan::default();
        for (expected_cursor, next_cursor) in [(0, 7), (7, 9)] {
            let count = get_backlinks_count(&mut state, |cursor| {
                assert_eq!(cursor, expected_cursor);
                Ok((next_cursor, vec![]))
            })
            .unwrap();
            assert_eq!(count, None);
            assert_eq!((state.cursor, state.count), (next_cursor, 0));
        }
        assert_eq!(
            get_backlinks_count(&mut state, |cursor| {
                assert_eq!(cursor, 9);
                Ok((0, vec![vec![]]))
            })
            .unwrap(),
            Some(1)
        );
    }

    #[test]
    fn completed_passes_reset_the_approximate_count() {
        let mut state = BacklinksScan::default();
        for _ in 0..2 {
            assert_eq!(
                get_backlinks_count(&mut state, |cursor| {
                    assert_eq!(cursor, 0);
                    Ok((0, vec![b"backlinks:duplicate".to_vec(); 2]))
                })
                .unwrap(),
                Some(2)
            );
            assert_eq!((state.cursor, state.count), (0, 0));
        }
        assert_eq!(
            get_backlinks_count(&mut state, |_| Ok((0, vec![]))).unwrap(),
            Some(0)
        );
    }

    #[test]
    fn scan_failures_preserve_progress_for_retry() {
        for (cursor, count) in [(0, 0), (7, 10)] {
            let mut state = BacklinksScan {
                cursor,
                count,
                pending_keys: 0,
            };
            for _ in 0..2 {
                let error = get_backlinks_count(&mut state, |requested_cursor| {
                    assert_eq!(requested_cursor, cursor);
                    Err((redis::ErrorKind::IoError, "test scan failure").into())
                })
                .unwrap_err();
                assert_eq!(error.kind(), redis::ErrorKind::IoError);
                assert_eq!(
                    (state.cursor, state.count, state.pending_keys),
                    (cursor, count, 0)
                );
            }
            assert_eq!(
                get_backlinks_count(&mut state, |requested_cursor| {
                    assert_eq!(requested_cursor, cursor);
                    Ok((0, vec![vec![]]))
                })
                .unwrap(),
                Some(count + 1)
            );
        }
    }

    #[test]
    fn oversized_pages_are_rejected_without_advancing_or_counting() {
        for (cursor, count) in [(0, 0), (7, 10)] {
            for next_cursor in [0, 11] {
                let mut state = BacklinksScan {
                    cursor,
                    count,
                    pending_keys: 0,
                };
                for _ in 0..2 {
                    let error = get_backlinks_count(&mut state, |requested_cursor| {
                        assert_eq!(requested_cursor, cursor);
                        Ok((next_cursor, vec![vec![]; 1025]))
                    })
                    .unwrap_err();
                    assert!(error
                        .to_string()
                        .contains("backlinks_scan_response_too_large"));
                    assert_eq!(
                        (state.cursor, state.count, state.pending_keys),
                        (cursor, count, 0)
                    );
                }
                assert_eq!(
                    get_backlinks_count(&mut state, |requested_cursor| {
                        assert_eq!(requested_cursor, cursor);
                        Ok((0, vec![vec![]]))
                    })
                    .unwrap(),
                    Some(count + 1)
                );
            }
        }
    }
}
