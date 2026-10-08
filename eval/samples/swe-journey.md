---
title: "Software Engineering"
created: 2026-07-12
tags: [go, architecture, best-practices, backend, software-design]
description: "Practical guidance on Go API contracts, interfaces, idempotency, logging, caching, brokers, and deployment."
seoTitle: "Software Engineering Practices for Reliable Go Backends"
seoDescription: "Practical guidance on Go API contracts, interfaces, idempotency, logging, caching, brokers, and deployment."
answerSummary: "These notes explain practical design choices that make Go backends easier to change, debug, operate, and scale."
---
	
# Software Engineering Practices for Reliable Go Backends

These notes explain practical design choices that make Go backends easier to change, debug, operate, and scale, from naming and interfaces to idempotency, caching, messaging, and deployment.

## Variable Naming & Function Design

Choose descriptive or short variable names based on function scope. Short names lose context in long functions.

A function name should show its intent, such as `GetProductDetail`. The name should make clear that it reads data, not changes it.

Use abstraction when a dependency may change. For example:

```go
func GetNews() (NewsResp) {
  // call reddit news
  // return NewesResp{}
}
```

If the news API changes, this function needs a rewrite. That logic gets harder to maintain when more APIs are added. An interface keeps the function independent of the provider:

```go
type NewsReq struct {}
type NewsResp struct {}

type NewsAPI interface {
  func GetNews(req NewsReq)(NewsResp, error)
}
```

`NewsReq` and `NewsResp` form a translation layer. Each provider maps its response to this shared format, so changing providers only changes the concrete implementation:

```go
// news/dependencies.go
func NewNews(n NewsAPI) {
}

// external/news.go
type RedditNews struct {}
type YahooNews struct {}

func (r *RedditNews) GetNews(req NewsReq) (NewsResp, error) {}
func (r *YahooNews) GetNews(req NewsReq) (NewsResp, error) {}

// server.go
import "news"

rn := &RedditNews{}
yn := &YahooNews{}

news := news.NewNews(rn)
// news := news.NewNews(yn)
```

## Abstraction with Interfaces

Add a compile-time type assertion to each concrete implementation. The compiler then catches interface mismatches:

```go
// external/news.go
package external

import "yourproject/news"

type RedditNews struct {}
type YahooNews struct {}

// Interface compliance checks (Compile-time type assertions)
var _ news.NewsAPI = (*RedditNews)(nil)
var _ news.NewsAPI = (*YahooNews)(nil)

func (r *RedditNews) GetNews(req news.NewsReq) (news.NewsResp, error) {
    return news.NewsResp{}, nil
}

func (y *YahooNews) GetNews(req news.NewsReq) (news.NewsResp, error) {
    return news.NewsResp{}, nil
}
```

## Data Types & API Contracts

Data types matter in API contracts. JavaScript's `Number` type safely represents integers only up to $9,007,199,254,740,991$.

Returning a larger integer can round and corrupt it in JavaScript. Send it as a **String** instead.

Floats also cause money calculations to lose precision. For example:

```go
package main

import "fmt"

func main() {
    var price float64 = 0.1
    var total float64 = 0.0

    // Add 0.1 ten times
    for i := 0; i < 10; i++ {
        total += price
    }

    // You expect 1.0, but float inaccuracy gives you: 0.9999999999999999
    fmt.Println("Total:", total)
}
```

Store money in the smallest currency unit, such as cents, using an integer. Integers avoid decimal rounding. See https://docs.stripe.com/api/charges/object.

| **Actual Amount** | **Value Stored in Database / Code (as Integer)** |
| --- | --- |
| $1.00 | `100` (cents) |
| $10.50 | `1050` (cents) |
| $99.99 | `9999` (cents) |

Go gives fields default zero values: `0` for integers, `0.0` for floats, and `""` for strings.

In finance, `0` can be meaningful. An admin fee of `0` differs from a missing or unconfigured fee.

```go
type FeeResponse struct {
    // If AdminFee is 0, omitempty deletes it from the JSON output!
    AdminFee int64 `json:"admin_fee,omitempty"`
}
```

Use `omitempty` carefully. Unlike primitive fields, an empty nested struct is **not** omitted; it is returned as `{}` with its zero values.

The standard `encoding/json` package treats `false`, `0`, a `nil` pointer, or an empty array, slice, map, or string as empty. An initialized struct such as `Address{}` is not on that list, so it serializes as `{}`.

## Structured Logging

CloudWatch and Datadog charge by ingested data and indexed events. Keep structured logs compact; write each JSON event on one line.

```go
// BAD: Wasteful multi-line logging (treated as 5+ log events)
{
  "request_id": "req-123",
  "status": 200,
  "path": "/v1/news",
  "latency_ms": 45
}

// GOOD: Compacted single-line structured logging (treated as 1 log event)
{"request_id":"req-123","status":200,"path":"/v1/news","latency_ms":45}
```

## Idempotency

**Idempotency** prevents retries from creating duplicate effects. It can use request hashes, unique IDs, or other keys. Repeating the same request should produce the same result as running it once.

## Context & Timeouts

Use `context.WithTimeout` to bound a request's lifetime. A specific internal operation may need a shorter timeout than the global default:

```go
func GetUserAccount(db *sql.DB, userID int) (*sql.Rows, error) {
	// Create a context that automatically cancels after 2 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Time(time.Second*2))
	defer cancel() // Free up resources once the function returns

	// Pass the context directly into the library function.
	// If the database takes longer than 2 seconds, the query cancels automatically.
	query := "SELECT id, balance FROM accounts WHERE id = ?"
	return db.QueryContext(ctx, query, userID)
}
```

## Caching Strategies

Choose between a distributed cache and local memory based on the use case.

For this blog, **in-memory storage** is enough because the data size is known. The main page uses less than 2 MB, so Go's `//go:embed` can load it into memory at compile time instead of using Redis.

```go
//go:embed blog_data.json
var blogContent []byte

func main() {
	// The 2MB of data is embedded in the binary.
	fmt.Println("Blog data size:", len(blogContent))
}
```

This makes startup a little slower but removes the **network hop** to an external database or cache.

I used the same approach for an external weather API, with a memory limit and Time-To-Live (TTL) expiration to bound cache growth.

Why skip Redis? A low-traffic company blog does not need another service. Local memory is faster, cheaper, and sufficient.

## Eager Initialization (Boot-time Singleton)

For static, predefined data, fetch it once at startup and keep it in a singleton. Consider memory use, concurrent access, and API failures. Ignore or default non-blocking failures; fail fast for required data.

```go
var (
    config     *StaticConfig
    configOnce sync.Once
)

// LoadConfig runs the fetch once, even with concurrent callers during boot.
func LoadConfig() *StaticConfig {
    configOnce.Do(func() {
        config = fetchFromRemoteAPI()
    })
    return config
}
```

## Message Broker Selection

Choose Kafka, RabbitMQ, NATS, or AWS SNS/SQS based on the use case, scale, and team experience. Kafka fits real-time streams and replayable append-only logs.

If the team knows AWS SNS/SQS well, using it may be the better choice. Operating a complex broker also adds cost and maintenance work.

If the system needs only pub/sub, **NATS** provides fast messaging with less operational work.

Choose based on fit and expected growth:
- **AWS SNS/SQS:** Managed asynchronous queuing with usage-based cost.
- **Kafka:** High-throughput streams, event sourcing, and replay for multiple consumers.
- **RabbitMQ:** Complex routing with exchanges and bindings.
- **NATS:** Low-latency pub/sub for lightweight services.

## Infrastructure & Observability

Infrastructure affects availability, durability, stability, and delivery speed. Observability such as distributed tracing helps correlate logs across services. Slow deployments and weak observability make software harder to operate.

Application developers also help keep the system healthy. Stable infrastructure cannot prevent code without timeouts from exhausting the system.

## Overthinking vs Underthinking

How far should you take a design before it becomes overthinking or underthinking?

Stop when more complexity is not worth the time, effort, or cost. An idempotency cache can grow like this:

1. You start with an **in-memory cache** for idempotency, but realize it won't survive an app crash.
2. So, you decide to use **Redis**. But what if Redis crashes?
3. You decide to add a **Redis Replica**. But what if data volume grows too big?
4. You plan for **Redis Sharding**. But what if an entire AWS region goes down?
5. You start designing **Geo-Sharding** and a full **Disaster Recovery plan**.

Planning for every failure at once is overkill.

| **Approach**      | **What it looks like**                                                                                                                                   | **The Risk**                                                                                                                         |
| ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| **Underthinking** | Throwing a quick fix together without considering basic failures (e.g., using a local map for idempotency in a multi-instance, autoscaling environment). | The app breaks immediately under standard production conditions.                                                                     |
| **Overthinking**  | Designing for "Six Nines" ($99.9999\%$) availability for a service that has low traffic or low business criticality.                                     | You waste months building complex infrastructure, delay the product launch, and create a system that is too complicated to maintain. |

Stop at the level that meets your **current constraints and next stage of growth**. Build the simplest safe version, then improve it when scale exposes a real problem. Do not solve problems you do not have yet; keep the design flexible.

## Choosing a SQL Database

When choosing an SQL database, compare its storage and indexing. PostgreSQL uses heap storage, so table data is stored separately from indexes. For example, compare PostgreSQL with SQL Server.

PostgreSQL indexes use a B-tree. A lookup finds the tuple ID (CTID), then reads the row from the heap.

SQL Server defaults to clustered indexes, where table data lives in the B-tree leaf nodes. Sequential primary keys can append new rows to the end and reduce page splits.


## Error Wrapping and Centered Logging

Logging every layer creates duplicate logs. Wrap errors with context as they move upward, then log the full chain once at the presentation layer, such as an HTTP handler.

```go
package main

import (
	"fmt"
	"log"
	"net/http"
)

// 1. Adapter Layer: Interacts with the database
func fetchUserFromDB(userID string) error {
	// Simulate a low-level database connection failure
	baseErr := fmt.Errorf("connection timed out") 
	return fmt.Errorf("database adapter failed: %w", baseErr)
}

// 2. Business Logic Layer: Processes core business rules
func GetUserProfile(userID string) error {
	err := fetchUserFromDB(userID)
	if err != nil {
		// Wrap the error with high-level business context
		return fmt.Errorf("failed to retrieve user profile for ID %s: %w", userID, err)
	}
	return nil
}

// 3. Presentation Layer: The entry point (HTTP API)
func UserHandler(w http.ResponseWriter, r *http.Request) {
	userID := "user_123"

	err := GetUserProfile(userID)
	if err != nil {
		// LOG ONCE: Captures the entire architectural journey of the failure
		log.Printf("[ERROR] API Request Failed: %v", err)
		
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func main() {
	// [ERROR] API Request Failed: failed to retrieve user profile for ID user_123: database adapter failed: connection timed out
}
```

## Concurrency Lifecycles and Failure Strategies

Concurrent code must account for deadlocks, missing timeouts, out-of-memory errors, data races, and invalid data access.

For one hundred API calls, choose the failure behavior first. Allow partial failures and let other calls finish, or cancel the whole batch on the first error with an error group.

Decide whether each call needs its own timeout and whether calls depend on one another. Use channels for dependent calls. Cancel contexts with `defer`, check channel closure, and define fallback behavior.

#### Example 1: Handling Partial Failures
Use this approach when all calls should finish, even if some fail.

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

func fetchWorker(ctx context.Context, url string, wg *sync.WaitGroup) {
	defer wg.Done()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		fmt.Printf("Error creating request for %s: %v\n", url, err)
		return
	}
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		// Log the error locally and allow other goroutines to keep running
		fmt.Printf("Error fetching %s: %v\n", url, err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Successfully fetched %s (Status: %d)\n", url, resp.StatusCode)
}

func main() {
	urls := []string{
		"https://httpbin.org/delay/1",
		"https://invalid-url-that-will-fail.com",
		"https://httpbin.org/delay/2",
	}

	var wg sync.WaitGroup
	
	// Create a global 5-second timeout so no operation hangs forever
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel() // Resource cleanup to prevent memory leaks

	for _, url := range urls {
		wg.Add(1)
		go fetchWorker(ctx, url, &wg)
	}

	wg.Wait()
	fmt.Println("All individual workers finished processing.")
}
```

#### Example 2: Stop Everything on First Error (Using errgroup.Group)

Use this approach for an all-or-nothing operation. An error group cancels the context when one call fails, so other workers can stop.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
)

func fetchCriticalData(ctx context.Context, id int) error {
	// Simulate an API call that fails specifically on ID 2
	if id == 2 {
		time.Sleep(500 * time.Millisecond)
		return errors.New("critical API dependency failed")
	}

	// Simulate a successful API call that takes 2 seconds
	select {
	case <-time.After(2 * time.Second):
		fmt.Printf("API call %d completed successfully\n", id)
		return nil
	case <-ctx.Done():
		// This triggers when another worker fails and cancels the context
		fmt.Printf("API call %d was aborted early\n", id)
		return ctx.Err()
	}
}

func main() {
	// Derive an error group from a base context
	g, ctx := errgroup.WithContext(context.Background())
	
	// Set a hard absolute timeout for the entire group
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	for i := 1; i <= 3; i++ {
		workerID := i
		// Launch the task inside the error group manager
		g.Go(func() error {
			return fetchCriticalData(ctx, workerID)
		})
	}

	// Wait blocks until all tasks finish OR the first error occurs
	if err := g.Wait(); err != nil {
		fmt.Printf("Batch processing stopped early due to error: %v\n", err)
		return
	}

	fmt.Println("Entire batch processing completed successfully.")
}
```

## Memory and Pointers

The `&` operator gets a variable's address. The `*` operator dereferences a pointer to read its value.

A pointer does not become `nil` because the garbage collector clears its data, or because of an out-of-memory event or crash. Go keeps pointed-to data alive while an active pointer references it.

A nil pointer error occurs when the pointer was never initialized to a valid address. An unmanaged out-of-memory error or severe system fault terminates the process; it does not reset pointer values.

The zero-value pointer has address `0x0`. Dereferencing it causes a runtime panic.

## String Header

When you initialize `test := "apple"`, Go represents the string with a 16-byte header on a 64-bit system:

- **Data pointer (8 bytes):** Address of the immutable byte array.
- **Length (8 bytes):** String size in bytes.

Passing or assigning a string copies its 16-byte header, not the text. Multiple headers can point to the same immutable backing array.

```go
package main

import (
	"fmt"
	"unsafe"
)

func main() {
	original := "apple"
	copied := original // Only the 16-byte header is duplicated here

	// 1. The headers live in separate locations on the stack
	fmt.Printf("Original header stack location: %p\n", &original)
	fmt.Printf("Copied header stack location:   %p\n\n", &copied)

	// 2. Both headers point to the same byte array in memory
	fmt.Printf("Original backing array pointer: %p\n", unsafe.StringData(original))
	fmt.Printf("Copied backing array pointer:   %p\n", unsafe.StringData(copied))
}
```

Go uses similar representations for other structural types:

- **Slices:** Passing a slice copies a 24-byte header containing a pointer, length, and capacity. Copies share the backing array, so changing elements changes the shared data.
- **Maps and channels:** Copies contain an address to shared runtime data.

**Primitives:** Integers, floats, and booleans are copied directly because their values are small.

Because strings, slices, and maps already use lightweight headers or pointers, **do not pass them as pointers (`*string`, `*[]int`, `*map`) just for performance.** Use a pointer only when you need to change the header itself.

## Stack vs. Heap
Whether a value uses the stack or heap depends on Go's escape analysis:

- **Passing by value:** Small values can remain in the local stack frame, which is discarded when the function returns.
- **Passing by pointer:** If the value may outlive the function, it escapes to the heap and is tracked by the garbage collector.

```go
package main

// A global variable that lives for the entire duration of the program
var globalStorage *int

func storePointer(p *int) {
	globalStorage = p // The pointer escapes the function scope here
}

func main() {
	// Declared locally inside main's stack frame
	num := 42 

	// Passing the pointer to a function that stores it globally.
	// The compiler cannot verify if 'num' will be safe on the stack 
	// after main finishes, so it escapes to the heap.
	storePointer(&num) 
}
```

Using unnecessary pointers can increase heap allocations and garbage-collector work. In long-running loops, that can increase memory use and cause an out-of-memory crash.

**Rule of thumb**: pass small values and header types by value; use pointers for large objects or state that must be modified.

## REST API
Use HTTP status codes correctly, such as `400` for bad requests. You can also return a business error code such as `"error_code: 23"`; keep it useful without exposing sensitive data.

Clients can use `404 Not Found` or read an application error from the response body, such as `cats: []`. The choice depends on the team's API standard.

Define the meaning of response fields clearly. For example:

- `"admin_fee: 0"` could mean something specific in finance.
- Mobile and web clients may interpret `"jelly: {}"` differently. Document whether an empty object is valid.

Treat every API dependency as unreliable. Schema validation defines the expected shape and catches `null`, empty, or invalid data early.

Even APIs under one base URL may differ in response time, authentication, headers, and cookies. Configure timeouts and request settings independently.
