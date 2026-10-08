---
title: OS Scheduling
description: "How N:1, 1:1, and M:N threading models map logical tasks to OS threads — goroutines, virtual threads, async tasks, and BEAM processes."
seoTitle: "How N:1, 1:1, and M:N Thread Scheduling Works"
seoDescription: "How language runtimes schedule work: N:1, 1:1, and M:N threading models, the Go GMP scheduler, Java virtual threads, Rust async, and BEAM processes."
answerSummary: "Language runtimes map many lightweight logical tasks onto a small pool of OS threads. N:1 stacks every task on one thread, 1:1 gives each task its own OS thread, and M:N multiplexes many tasks over many threads — Go, Java virtual threads, Rust async, and BEAM all use M:N, differing in where tasks can pause and how blocked threads are handled."
aliases: []
tags:
  - cs
  - os
created: 2026-06-13
---

# How N:1, 1:1, and M:N Scheduling Actually Work

**Logical task** means a unit of work managed by the language or runtime. A goroutine, Java virtual thread, Rust async task, or BEAM process can all be logical tasks. **OS thread** means a native thread known and scheduled by the operating system.

A logical task is not always an OS thread. Sometimes one program thread maps directly to one OS thread, but sometimes many logical tasks share a smaller set of OS threads.

For an `M:N` runtime, the full stack looks like this. The runtime scheduler layer is what `N:1` and `M:N` add; a `1:1` program goes straight from app threads to the OS:

```
Logical tasks            goroutines, virtual threads, async tasks, BEAM processes
     |
     v
Runtime scheduler        Go runtime, JVM scheduler, Tokio, BEAM schedulers
     |
     v
OS threads               Ms, carrier threads, worker threads
     |
     v
OS scheduler
     |
     v
CPU cores
```

The runtime decides **which logical task should run**, while the OS decides **which OS thread gets CPU time**. That is the main difference behind `N:1`, `1:1`, and `M:N`.

|Model|Mapping|Multi-core?|Common examples|
|---|---|---|---|
|**N:1**|Many logical tasks → one OS thread|No|Old green-thread runtimes, Node.js event loop, Tokio `current_thread`, Ruby 1.8 and earlier|
|**1:1**|One app thread → one OS thread|Yes|Rust `std::thread`, Java platform threads|
|**M:N**|Many logical tasks → many OS threads|Yes|Go, Java virtual threads, Rust async, BEAM|

The three models look like this:

```
N:1

Logical Task A -+
Logical Task B -+
Logical Task C -+--- Runtime ---- OS Thread ---- CPU
Logical Task D -+
Logical Task E -+


1:1

App Thread A ---- OS Thread A -+
App Thread B ---- OS Thread B -+--- OS Scheduler --- CPU Cores
App Thread C ---- OS Thread C -+
App Thread D ---- OS Thread D -+


M:N

Logical Task A -+                +-- OS Thread 1 -+
Logical Task B -+                +-- OS Thread 2 -+
Logical Task C -+--- Runtime ----+                +--- CPU Cores
Logical Task D -+                +-- OS Thread 3 -+
Logical Task E -+                +-- OS Thread 4 -+
```

## N:1

`N:1` means many logical tasks share one OS thread. The runtime can manage hundreds or thousands of logical tasks, but the OS only sees one native thread. This makes task switching cheap because the runtime can move from one logical task to another in user space. The problem is that all work still goes through one OS thread. If the machine has eight CPU cores but the runtime only has one OS thread, the app still cannot use all eight cores at the same time.

```
1000 logical tasks
        |
        v
runtime
        |
        v
1 OS thread
        |
        v
1 CPU core at a time
```

So `N:1` gives concurrency, but not real multi-core parallelism. Blocking is also a problem. If the only OS thread enters a blocking syscall, other logical tasks may be ready but cannot run because there is no second thread available.

## 1:1

`1:1` is different because the app-level thread and the OS thread are almost the same execution unit from a scheduling point of view. Each app thread maps to one OS thread.

```
App Thread A ---- OS Thread A
App Thread B ---- OS Thread B
App Thread C ---- OS Thread C
App Thread D ---- OS Thread D
```

It is better to say **app thread** instead of logical task because the application thread already has its own native OS thread. The OS does most of the scheduling. If the machine has four cores and the program has four runnable threads, the OS can run them in parallel. The downside is that every app thread also means another OS thread.

### Rust `std::thread`

Rust's normal threads use the `1:1` model.

```
std::thread::spawn(|| {
    do_work();
});
```

In this case, the Rust thread is not a lightweight logical task being multiplexed over another worker pool. It directly maps to a native thread.

### Java Platform Threads

Java platform threads follow the same general model.

```
new Thread(() -> {
    doWork();
}).start();
```

## M:N

`M:N` means many logical tasks run on multiple OS threads, usually with far more logical tasks than native threads. For example:

```
100,000 logical tasks
          |
          v
runtime scheduler
          |
          v
8 OS threads
          |
          v
OS scheduler
          |
          v
8 CPU cores
```

Every runtime below has the same shape as the stack at the top of this page; only the names change.

A logical task is something the runtime can pause, queue, resume, or move between worker threads. Pausing usually happens only at well-defined points, though: 
- a Rust Tokio task stops only at `.await`
- a virtual thread unmounts only when it blocks on something
- Go needed until 1.14 to preempt goroutines stuck in tight loops. 
- BEAM is the exception — it preempts processes by counting reductions, so Erlang code cannot starve a scheduler.

The OS does not schedule that logical task directly. The OS only schedules the native thread that eventually runs it.

### Go Scheduler

A goroutine is a logical task, not an OS thread. When you write `go handleConnection(conn)` the Go runtime creates a goroutine. That goroutine is scheduled by Go and eventually runs on an OS thread. Go often describes the scheduler with:

```
G = goroutine — the task
M = OS thread — the worker that runs it
P = worker slot with a to-do list — not a process, not a CPU core
```

So `G` is the logical task, while `M` is the native thread. Goroutines always run on an `M`, never on a `P` — the `P` is only where goroutines wait in line, and an `M` has to hold a `P` before it can run any.

```
             Go Runtime

G1 -+
G2 -+        P0 --- M0 -+
G3 -+                   |
G4 -+        P1 --- M1 -+--- OS Scheduler --- CPU Cores
G5 -+                   |
G6 -+        P2 --- M2 -+
... |                   |
Gn -+        P3 --- M3 -+
```

This is why a program can have 100,000 goroutines without needing 100,000 OS threads.

Think of a `P` as a worker slot that holds a to-do list of goroutines, and an `M` as the worker thread standing at that slot. The slot stays; the worker can change. If a goroutine makes a blocking syscall, its `M` gets stuck waiting, but the runtime moves the `P` (to-do list and all) to another `M`, so the goroutines waiting on that `P` keep running. One blocked goroutine costs one thread, not the whole program. That is how M:N solves the N:1 blocking problem. The number of `P`s is `GOMAXPROCS` — it decides how many goroutines can truly run at the same time, and it defaults to the number of CPU cores.

### Java Virtual Threads

A Java virtual thread is also a logical task. It is managed by the JVM and runs on carrier threads — normal platform threads owned by the JVM's scheduler (a ForkJoinPool by default), which decides which virtual thread gets mounted on which carrier.

### Rust Async

A Rust async task is also a logical task. An executor such as Tokio schedules many async tasks over worker threads. The async task is not itself an OS thread. It is work managed by the executor. The worker thread is the native thread that actually runs the task.

### Elixir and Erlang

A BEAM process is another example of a logical task. It is not an OS process and not an OS thread. The BEAM scheduler manages many BEAM processes and runs them on scheduler threads, one per CPU core by default.
